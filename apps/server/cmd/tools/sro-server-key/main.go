/*
===========================================================================

main.go - sro-server-key: the licensor's server key tool

The genkey command creates the licensor's Ed25519 key pair: the private
file stays offline with the licensor, and the printed public key goes into
internal/security/serverkey/licensor.go. The sign command issues a key for
an operator's domains, valid 30 days unless -days says otherwise (at most
90). The show command verifies a key against the compiled-in licensor key
and prints what it grants.

===========================================================================
*/
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"opensro.online/server/internal/security/serverkey"
)

const privateKeyPrefix = "opensro-licensor-ed25519:"

/*
================
main
================
*/
func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "genkey":
		err = genkey(os.Args[2:])
	case "sign":
		err = sign(os.Args[2:])
	case "show":
		err = show(os.Args[2:])
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "sro-server-key:", err)
		os.Exit(1)
	}
}

/*
================
usage
================
*/
func usage() {
	fmt.Fprintln(os.Stderr, "usage: sro-server-key genkey -out <private file>")
	fmt.Fprintln(os.Stderr, "       sro-server-key sign -licensor <private file> -operator <name> -domains <a,b> [-days 30] -out <key file>")
	fmt.Fprintln(os.Stderr, "       sro-server-key show <key file>")
	os.Exit(2)
}

/*
================
genkey

Refuses to overwrite: losing the licensor's private key invalidates every
key issued with it.
================
*/
func genkey(arguments []string) error {
	flags := flag.NewFlagSet("genkey", flag.ExitOnError)
	out := flags.String("out", "", "private key file to create")
	_ = flags.Parse(arguments)
	if *out == "" {
		return errors.New("genkey needs -out")
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(*out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(file, privateKeyPrefix+base64.StdEncoding.EncodeToString(private))
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	fmt.Println("public key:", base64.StdEncoding.EncodeToString(public))
	return nil
}

/*
================
readPrivateKey
================
*/
func readPrivateKey(path string) (ed25519.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	encoded, ok := strings.CutPrefix(strings.TrimSpace(string(data)), privateKeyPrefix)
	if !ok {
		return nil, fmt.Errorf("%s is not a licensor private key", path)
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(raw) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("%s is malformed", path)
	}
	return ed25519.PrivateKey(raw), nil
}

/*
================
sign
================
*/
func sign(arguments []string) error {
	flags := flag.NewFlagSet("sign", flag.ExitOnError)
	licensor := flags.String("licensor", "", "licensor private key file")
	operator := flags.String("operator", "", "who the key is issued to")
	domains := flags.String("domains", "", "comma-separated public domains (subdomains included)")
	days := flags.Int("days", int(serverkey.DefaultLifetime/(24*time.Hour)), "lifetime in days")
	out := flags.String("out", "", "key file to write")
	_ = flags.Parse(arguments)
	if *licensor == "" || *out == "" {
		return errors.New("sign needs -licensor and -out")
	}
	private, err := readPrivateKey(*licensor)
	if err != nil {
		return err
	}
	issued := time.Now().UTC().Truncate(time.Second)
	data, err := serverkey.Sign(serverkey.Grant{
		Operator: strings.TrimSpace(*operator),
		Domains:  strings.Split(*domains, ","),
		Issued:   issued,
		Expires:  issued.Add(time.Duration(*days) * 24 * time.Hour),
	}, private)
	if err != nil {
		return err
	}
	if err := os.WriteFile(*out, append(data, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s, expires %s\n", *out, issued.Add(time.Duration(*days)*24*time.Hour).Format(time.RFC3339))
	return nil
}

/*
================
show
================
*/
func show(arguments []string) error {
	if len(arguments) != 1 {
		usage()
	}
	data, err := os.ReadFile(arguments[0])
	if err != nil {
		return err
	}
	licensor, err := serverkey.LicensorKey()
	if err != nil {
		return err
	}
	grant, err := serverkey.Parse(data, licensor)
	if err != nil {
		return err
	}
	fmt.Printf("operator %s\ndomains  %s\nissued   %s\nexpires  %s\n",
		grant.Operator,
		strings.Join(grant.Domains, ", "),
		grant.Issued.Format(time.RFC3339),
		grant.Expires.Format(time.RFC3339),
	)
	return nil
}
