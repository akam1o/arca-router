package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/akam1o/arca-router/pkg/netconf"
)

func main() {
	var (
		dbPath   string
		username string
		password string
		role     string
	)

	flag.StringVar(&dbPath, "path", "", "path to the NETCONF user database")
	flag.StringVar(&username, "username", "", "NETCONF username to create")
	flag.StringVar(&password, "password", "", "plain-text NETCONF password to store as an argon2id hash")
	flag.StringVar(&role, "role", netconf.RoleAdmin, "NETCONF role: admin, operator, or read-only")
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
}
