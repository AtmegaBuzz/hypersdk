/*
Copyright © 2023 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"errors"
	"fmt"
	"log"

	"github.com/boltdb/bolt"
	"github.com/spf13/cobra"
)

// aliasCmd represents the alias command
var aliasCmd = &cobra.Command{
	Use: "alias",
	RunE: func(*cobra.Command, []string) error {
		return ErrMissingSubcommand
	},
}

var addAliasCmd = &cobra.Command{
	Use: "add",
	RunE: func(cmd *cobra.Command, args []string) error {

		if len(args) < 2 {
			return errors.New("Please provide both alias_name and address")
		}

		alias_name := args[0]
		address := args[1]

		return AddAliasFunc(alias_name, address)
	},
}

func AddAliasFunc(alias_name string, address string) error {

	db, err := bolt.Open("tokenvm.db", 0600, nil)

	if err != nil {
		log.Fatal(err)
	}

	appendAlias(db, alias_name, address)

	addrValue, err := ResolveAlias(db, alias_name)

	fmt.Printf("Alias added for %s Value: %s\n", alias_name, addrValue)

	defer db.Close()

	return nil
}

func ResolveAlias(db *bolt.DB, key string) (string, error) {

	var resolvedAliasValue string = ""

	err := db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte("AliasBucket"))
		if bucket == nil {
			return nil // Bucket does not exist
		}

		resolvedAliasValue = string(bucket.Get([]byte(key)))

		return nil
	})

	if err != nil {
		log.Fatal(err)
	}

	return resolvedAliasValue, nil

}

func appendAlias(db *bolt.DB, key string, value string) error {

	err := db.Update(func(tx *bolt.Tx) error {
		// Create or retrieve a bucket (analogous to a table in SQL)
		bucket, err := tx.CreateBucketIfNotExists([]byte("AliasBucket"))
		if err != nil {
			return err
		}

		// Store key-value pairs
		if err := bucket.Put([]byte(key), []byte(value)); err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		log.Fatal(err)
	}

	return nil

}
