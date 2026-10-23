package enterworld

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"opensro.online/server/internal/testsupport/licensed"
	"os"
	"strconv"
	"testing"

	"opensro.online/server/internal/game/item/statuseffect"
)

func replacementFields(program []uint32) []string {
	f := make([]string, 118)
	for i := range f {
		f[i] = "0"
	}
	f[2], f[5], f[7], f[18], f[68] = "23", "BASIC_CODE", "7", "4278387201", "3"
	for i, v := range program {
		f[69+i] = strconv.FormatUint(uint64(v), 10)
	}
	return f
}

func TestReplacementMetadataNativeExecution(t *testing.T) {
	var report struct {
		CandidateHash string `json:"candidate_sha256"`
		SchemaHash    string `json:"program_schema_sha256"`
		Cases         []struct {
			Name     string
			Program  []uint32
			Expected statuseffect.ReplacementDescriptor
		}
	}
	raw, err := licensed.ServerFixture(t, "internal/game/enterworld/testdata/native-replacement-metadata.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatal(err)
	}
	for path, expected := range map[string]string{"skillreplacement.go": report.CandidateHash, "spawnskillparams.go": report.SchemaHash} {
		source, err := os.ReadFile(path)
		if err != nil || fmt.Sprintf("%x", sha256.Sum256(source)) != expected {
			t.Fatalf("%s changed since native comparison; regenerate evidence: %v", path, err)
		}
	}
	if len(report.Cases) != 93 {
		t.Fatal("native branch corpus changed", len(report.Cases))
	}
	for _, c := range report.Cases {
		t.Run(c.Name, func(t *testing.T) {
			got, err := compileSkillReplacement(replacementFields(c.Program))
			// Column bindings are checked separately from native encoded-tail execution.
			c.Expected.Category, c.Expected.Group, c.Expected.Rank = 3, 23, 7
			c.Expected.BasicCode, c.Expected.PackedStates = "BASIC_CODE", 4278387201
			if err != nil || got != c.Expected {
				t.Fatalf("got %+v, error %v; native %+v", got, err, c.Expected)
			}
		})
	}
}

func TestReplacementMetadataRejectsInvalidRecords(t *testing.T) {
	for _, c := range []struct {
		col   int
		value string
	}{
		{2, "-1"}, {5, ""}, {7, "256"}, {7, "-1"}, {18, "4294967296"},
		{18, "invalid"}, {68, "-1"}, {69, "1234567"}, {117, "6386804"},
	} {
		f := replacementFields(nil)
		f[c.col] = c.value
		if got, err := compileSkillReplacement(f); err == nil || got != (statuseffect.ReplacementDescriptor{}) {
			t.Fatalf("invalid column %d=%q returned %+v, %v", c.col, c.value, got, err)
		}
	}
	if _, err := compileSkillReplacement(make([]string, 69)); err == nil {
		t.Fatal("incomplete catalog fixture acquired replacement authority")
	}
}

func TestReplacementMetadataLoadedTable(t *testing.T) {
	skills := sharedShippedSkills(t)
	if err := skills.Load(); err != nil {
		t.Fatal(err)
	}
	decoded, refused := 0, 0
	for id, row := range skills.rows.values() {
		if !row.ReplacementPinned {
			refused++
			if row.ReplacementRefusal == "" || row.Replacement != (statuseffect.ReplacementDescriptor{}) {
				t.Fatalf("skill %d has an unclassified or partial projection", id)
			}
			continue
		}
		decoded++
		if row.ReplacementRefusal != "" || row.Replacement.Group != row.Group || int64(row.Replacement.Rank) != row.Level {
			t.Fatalf("skill %d replacement identity disagrees with catalog", id)
		}
	}
	if decoded == 0 {
		t.Fatal("no supplied skill metadata decoded")
	}
	t.Logf("loaded replacement metadata: %d decoded, %d explicitly refused; no execution admission implied", decoded, refused)
}
