package main

import (
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/urfave/cli"

	"github.com/toaster/fritz_sync/sync/carddav"
)

func main() {
	app := cli.NewApp()
	app.Usage = "perform sync adapter actions"
	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:  "url, e",
			Usage: "`URL` of the sync endpoint",
		},
		cli.StringFlag{
			Name:  "user, u",
			Usage: "`USERNAME`",
		},
		cli.StringFlag{
			Name:  "password, p",
			Usage: "`PASSWORD`",
		},
		cli.StringSliceFlag{
			Name:  "read, r",
			Usage: "`CATEGORIES` is an optional list of categories",
		},
	}
	app.Action = func(ctx *cli.Context) error {
		url := ctx.String("url")
		user := ctx.String("user")
		pass := ctx.String("password")
		readCategories := ctx.StringSlice("read")

		if url == "" {
			return errors.New("you have to specify the URL")
		}
		if user == "" {
			return errors.New("you have to specify the user")
		}
		if pass == "" {
			return errors.New("you have to specify the password")
		}
		if readCategories == nil {
			return errors.New("you have to specify an action")
		}

		adapter := carddav.NewAdapter(url, user, pass)

		contacts, err := adapter.ReadAll(readCategories)
		if err != nil {
			return fmt.Errorf("read failed: %v", err)
		}
		fmt.Println("contacts", contacts)
		for key, contact := range contacts {
			fmt.Println("Key:", key)
			fmt.Println("Contact:", contact)
		}
		return nil
	}
	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}
