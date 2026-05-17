package main

import (
	"encoding/base64"
	"flag"
	"fmt"
	"os"

	"github.com/akam1o/arca-router/pkg/netconf"
	"golang.org/x/crypto/ssh"
)

func main() {
	var (
		dbPath           string
		username         string
		password         string
		role             string
		publicKeyFile    string
		publicKeyComment string
	)

	flag.StringVar(&dbPath, "path", "", "path to the NETCONF user database")
	flag.StringVar(&username, "username", "", "NETCONF username to create")
	flag.StringVar(&password, "password", "", "plain-text NETCONF password to store as an argon2id hash")
	flag.StringVar(&role, "role", netconf.RoleAdmin, "NETCONF role: admin, operator, or read-only")
	flag.StringVar(&publicKeyFile, "public-key-file", "", "optional OpenSSH authorized_keys public key to add for the user")
	flag.StringVar(&publicKeyComment, "public-key-comment", "", "optional comment override for -public-key-file")
	flag.Parse()

	if dbPath == "" || username == "" || password == "" {
		fmt.Fprintln(os.Stderr, "-path, -username, and -password are required")
		os.Exit(2)
	}

	hash, err := netconf.HashPassword(password)
	if err != nil {
		fmt.Fprintf(os.Stderr, "hash password: %v\n", err)
		os.Exit(1)
	}

	userDB, err := netconf.NewUserDatabase(dbPath, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open user database: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		if err := userDB.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "close user database: %v\n", err)
			os.Exit(1)
		}
	}()

	if err := userDB.CreateUser(username, hash, role); err != nil {
		fmt.Fprintf(os.Stderr, "create user: %v\n", err)
		os.Exit(1)
	}

	if publicKeyFile != "" {
		key, comment, err := readAuthorizedPublicKey(publicKeyFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "read public key: %v\n", err)
			os.Exit(1)
		}
		if publicKeyComment != "" {
			comment = publicKeyComment
		}
		keyData := base64.StdEncoding.EncodeToString(key.Marshal())
		if err := userDB.AddPublicKey(username, key.Type(), keyData, ssh.FingerprintSHA256(key), comment); err != nil {
			fmt.Fprintf(os.Stderr, "add public key: %v\n", err)
			os.Exit(1)
		}
	}
}

func readAuthorizedPublicKey(path string) (ssh.PublicKey, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	key, comment, _, _, err := ssh.ParseAuthorizedKey(data)
	if err != nil {
		return nil, "", err
	}
	return key, comment, nil
}
