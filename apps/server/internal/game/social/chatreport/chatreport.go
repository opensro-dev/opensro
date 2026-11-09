/*
===========================================================================

chatreport.go - /report: a player's chat-abuse report to staff triage

Port-only, not native (#2959, following #2671's moderation tooling): a
chat line "/report <character> [reason]" never reaches the room. The
reporter gets one line back, and the report - reporter, target, reason,
and the pair's recent lines from the staff chat log (chatlog), the
target's marked - is delivered through the bug-report flow
(agent/bugreport): the same sink, embed shape and staff triage the
in-game bug reporter already feeds.

The service exists only where a bug-report sink reaches this process
(the Agent's SRO_BUG_REPORT_* variables, bugreport.LoadConfig); with
none configured the wiring leaves it nil and "/report ..." stays
ordinary chat text, which is the native posture for a command the
original does not have.

File answers at once and never blocks on the sink or the disk: the
rate-limit stamp is the only synchronous work, and one worker goroutine
gathers the context and delivers, behind a bounded queue. A report the
queue cannot take is dropped and counted, like a chatlog line: losing
one beats stalling a chat handler.

===========================================================================
*/

package chatreport

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/agent/bugreport"
	"opensro.online/server/internal/game/social/chatlog"
)

const (
	// reportCooldown paces one account's reports. Staff triage is read
	// by humans and one report already carries the chat context, so a
	// shorter gap would only multiply near-duplicates; five minutes
	// still lets a player report a second abuser the same evening.
	reportCooldown = 5 * time.Minute
	// contextLines is how many of the pair's recent lines ride with a
	// report: enough for staff to see the exchange that prompted it,
	// few enough to stay inside the report's embed budget.
	contextLines = 20
	// contextWindow bounds the log scan to the last week of daily
	// files; a report is about recent abuse, and the whole log stays
	// searchable from the staff console either way.
	contextWindow = 7 * 24 * time.Hour
	// reasonMaxRunes bounds the free-text reason: it rides in the
	// report's description and one embed field, both staff-sized.
	reasonMaxRunes = 500
	// contextFieldMaxRunes keeps one embed field under Discord's
	// 1024-character field cap (bugreport's maxFieldValue).
	contextFieldMaxRunes = 1000
	// contextFieldsMax bounds the context fields per report, so the
	// chat context cannot crowd the identity fields out of the embed.
	contextFieldsMax = 3
	// queueSize bounds reports waiting on the worker; past it File
	// refuses and counts the drop.
	queueSize = 16
)

// The one-line answers. Every /report line gets exactly one, and
// nothing is sent to anyone else.
const (
	reportUsage   = "Usage: /report <character> [reason]"
	reportSelf    = "You cannot report yourself."
	reportLimited = "You have reported recently; please wait a few minutes before reporting again."
	reportBusy    = "Reports are busy right now; please try again in a moment."
)

/*
================
Deliverer

The bug-report sink (agent/bugreport's Service satisfies it): one
report in, the delivery id out.
================
*/
type Deliverer interface {
	Deliver(ctx context.Context, report bugreport.Report) (string, error)
}

/*
================
Resolver

A character name answered with the account behind it and the stored
spelling, when the division's live records know the name. An unknown
name still files; staff meet the name as typed.
================
*/
type Resolver func(divisionID, name string) (account, canonical string, found bool)

/*
================
Reporter

Who filed: the identity the chat handler resolved from the session.
================
*/
type Reporter struct {
	AccountID string
	Character string
	Division  string
}

// job is one accepted report on its way to the sink.
type job struct {
	reporter Reporter
	target   string
	reason   string
}

/*
================
Service

The /report intake for one chatlog directory. File is safe from any
goroutine.
================
*/
type Service struct {
	sink    Deliverer
	logDir  string
	resolve Resolver
	now     func() time.Time

	mu      sync.Mutex
	last    map[string]time.Time
	queue   chan job
	dropped atomic.Int64
}

/*
================
New

An intake delivering through sink, reading chat context from logDir,
already working. resolve may be nil (no name is ever resolved).
================
*/
func New(sink Deliverer, logDir string, resolve Resolver) *Service {
	service := &Service{
		sink:    sink,
		logDir:  logDir,
		resolve: resolve,
		now:     time.Now,
		last:    make(map[string]time.Time),
		queue:   make(chan job, queueSize),
	}
	go service.run()
	return service
}

/*
================
FromEnv

The intake for this process, or nil: the bug-report configuration the
Agent reads (bugreport.LoadConfig over getenv) decides. No sink, no
service - bug reports are never worth an outage, and chat reports
share that posture. Warnings go to the log; they never carry the
webhook.
================
*/
func FromEnv(getenv func(string) string, logDir string, resolve Resolver) *Service {
	config, warnings := bugreport.LoadConfig(getenv)
	for _, warning := range warnings {
		log.Warn("chatreport: " + warning)
	}
	if !config.Enabled() {
		return nil
	}
	sink, err := bugreport.New(config, nil, nil)
	if err != nil {
		log.WithError(err).Warn("chatreport: bug-report sink refused; /report stays off")
		return nil
	}
	return New(sink, logDir, resolve)
}

/*
================
Dropped

Reports the bounded queue could not take since the service opened.
================
*/
func (service *Service) Dropped() int64 {
	if service == nil {
		return 0
	}
	return service.dropped.Load()
}

/*
================
File

Answer one chat line. Handled is false when the line is not the
command (ordinary chat text, which the caller routes as ever). A
handled line is consumed whole: one of the one-line answers comes
back, and an accepted report is queued for delivery.
================
*/
func (service *Service) File(reporter Reporter, message string) (answer string, handled bool) {
	fields := strings.Fields(message)
	if len(fields) == 0 || !strings.EqualFold(fields[0], "/report") {
		return "", false
	}
	if len(fields) < 2 {
		return reportUsage, true
	}
	target := fields[1]
	if strings.EqualFold(target, reporter.Character) {
		return reportSelf, true
	}
	reason := strings.Join(fields[2:], " ")
	if utf8.RuneCountInString(reason) > reasonMaxRunes {
		reason = string([]rune(reason)[:reasonMaxRunes])
	}
	now := service.now()
	service.mu.Lock()
	if last, seen := service.last[reporter.AccountID]; seen && now.Sub(last) < reportCooldown {
		service.mu.Unlock()
		return reportLimited, true
	}
	service.last[reporter.AccountID] = now
	service.mu.Unlock()
	select {
	case service.queue <- job{reporter: reporter, target: target, reason: reason}:
		return fmt.Sprintf("Report filed against %s. Staff will review it.", target), true
	default:
		// Nothing was filed, so the stamp must not pace a retry.
		service.mu.Lock()
		if service.last[reporter.AccountID] == now {
			delete(service.last, reporter.AccountID)
		}
		service.mu.Unlock()
		service.dropped.Add(1)
		return reportBusy, true
	}
}

/*
================
run

The one worker: build each report (context from the log, delivery
through the sink) in the order filed. A delivery failure warns with
the identities, so the server log stays the map from a report to its
accounts, as the bug-report flow's directory copy and Agent log are.
================
*/
func (service *Service) run() {
	for filed := range service.queue {
		report, targetAccount := service.build(filed)
		id, err := service.sink.Deliver(context.Background(), report)
		if err != nil {
			log.WithError(err).WithFields(log.Fields{
				"reporter": filed.reporter.Character, "reporterAccount": filed.reporter.AccountID,
				"target": filed.target, "targetAccount": targetAccount,
			}).Warn("chatreport: delivery failed; the report is lost")
			continue
		}
		log.WithFields(log.Fields{
			"reporter": filed.reporter.Character, "reporterAccount": filed.reporter.AccountID,
			"target": filed.target, "targetAccount": targetAccount, "delivery": id,
		}).Info("chatreport: report filed")
	}
}

/*
================
build

One bug-report-shaped Report from a filed job, plus the target's
account for the server log (accounts never enter the report's Discord
side; bugreport's embed carries character names only). The target's
stored spelling wins over the typed one when it resolves.
================
*/
func (service *Service) build(filed job) (bugreport.Report, string) {
	target := filed.target
	targetAccount := ""
	if service.resolve != nil {
		if account, canonical, found := service.resolve(filed.reporter.Division, filed.target); found {
			target, targetAccount = canonical, account
		}
	}
	description := "Chat abuse report: " + target
	if filed.reason != "" {
		description += "\nReason: " + filed.reason
	}
	reason := filed.reason
	if reason == "" {
		reason = "(no reason given)"
	}
	fields := []bugreport.Field{
		{Name: "Reported player", Value: target},
		{Name: "Reason", Value: reason},
	}
	fields = append(fields, service.contextFields(filed, target)...)
	return bugreport.Report{
		Account:   filed.reporter.AccountID,
		Division:  filed.reporter.Division,
		Character: filed.reporter.Character,
		Submission: bugreport.Submission{
			Description: description,
			Context:     fields,
		},
	}, targetAccount
}

/*
================
contextFields

The pair's last lines from the staff chat log (chatlog.Search), oldest
first, the target's lines marked "»": a line counts when either of
them said it, and a whisper counts only between the two. The lines
pack into as few embed fields as they fit, bounded like the flow's
own fields.
================
*/
func (service *Service) contextFields(filed job, target string) []bugreport.Field {
	reporter := filed.reporter.Character
	now := service.now()
	query := chatlog.Query{From: now.Add(-contextWindow), Limit: contextLines}
	byReporter, reporterErr := chatlog.Search(service.logDir, withCharacter(query, reporter))
	byTarget, targetErr := chatlog.Search(service.logDir, withCharacter(query, target))
	if reporterErr != nil || targetErr != nil {
		return []bugreport.Field{{Name: "Chat context", Value: "(the chat context could not be read)"}}
	}
	type lineAt struct {
		at   time.Time
		text string
	}
	var lines []lineAt
	seen := make(map[string]bool)
	for _, result := range []chatlog.Result{byReporter, byTarget} {
		for _, entry := range result.Lines {
			at, err := time.Parse(time.RFC3339, entry.TS)
			if err != nil {
				continue
			}
			mine := strings.EqualFold(entry.Character, reporter)
			theirs := strings.EqualFold(entry.Character, target)
			if !mine && !theirs {
				continue
			}
			if entry.Channel == "whisper" &&
				!strings.EqualFold(entry.Target, reporter) && !strings.EqualFold(entry.Target, target) {
				continue
			}
			key := entry.TS + "\x00" + entry.Character + "\x00" + entry.Text
			if seen[key] {
				continue
			}
			seen[key] = true
			mark := "  "
			if theirs {
				mark = "» "
			}
			text := mark + "[" + at.Format("15:04") + "] " + entry.Character
			if entry.Channel == "whisper" {
				text += " (whisper)"
			}
			lines = append(lines, lineAt{at: at, text: text + ": " + entry.Text})
		}
	}
	if len(lines) == 0 {
		return []bugreport.Field{{Name: "Chat context", Value: "(no recent lines between them in the chat log)"}}
	}
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].at.Before(lines[j].at) })
	if len(lines) > contextLines {
		lines = lines[len(lines)-contextLines:]
	}
	var fields []bugreport.Field
	var block strings.Builder
	flush := func() {
		if block.Len() == 0 {
			return
		}
		name := "Chat context"
		if len(fields) > 0 {
			name = fmt.Sprintf("Chat context (%d)", len(fields)+1)
		}
		fields = append(fields, bugreport.Field{Name: name, Value: strings.TrimSuffix(block.String(), "\n")})
		block.Reset()
	}
	for _, line := range lines {
		if len(fields) >= contextFieldsMax {
			break
		}
		if block.Len() > 0 && block.Len()+len(line.text)+1 > contextFieldMaxRunes {
			flush()
			if len(fields) >= contextFieldsMax {
				break
			}
		}
		block.WriteString(line.text)
		block.WriteByte('\n')
	}
	flush()
	return fields
}

/*
================
withCharacter
================
*/
func withCharacter(query chatlog.Query, character string) chatlog.Query {
	query.Character = character
	return query
}
