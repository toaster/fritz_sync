package main

import (
	"errors"
	"log"
	"os"

	"github.com/urfave/cli"

	"github.com/toaster/fritz_sync/sync"
	"github.com/toaster/fritz_sync/sync/carddav"
	"github.com/toaster/fritz_sync/sync/fritzbox"
)

func main() {
	app := cli.NewApp()
	app.Usage = "sync contacts from CardDAV to Fritz!Box"
	app.Flags = []cli.Flag{
		cli.StringSliceFlag{
			Name:  "carddav_url, c",
			Usage: "`URL` of the CardDAV contacts; may be specified multiple times",
		},
		cli.StringFlag{
			Name:  "carddav_user, cu",
			Usage: "`USERNAME` to connect to the CardDAV server",
		},
		cli.StringFlag{
			Name:  "carddav_password, cp",
			Usage: "`PASSWORD` to connect to the CardDAV server",
		},
		cli.StringSliceFlag{
			Name:  "carddav_category, cc",
			Usage: "`CATEGORY` to synchronize; may be specified multiple times, all categories are synced if omitted",
		},
		cli.StringFlag{
			Name:  "fritz_url, f",
			Usage: "`URL` of the Fritz!Box",
		},
		cli.StringFlag{
			Name:  "fritz_phonebook, p",
			Usage: "`NAME` of the target phonebook at the Fritz!Box",
		},
		cli.StringFlag{
			Name:  "fritz_user, fu",
			Usage: "`USERNAME` to connect to the Fritz!Box",
		},
		cli.StringFlag{
			Name:  "fritz_password, fp",
			Usage: "`PASSWORD` to connect to the Fritz!Box",
		},
		cli.StringFlag{
			Name:  "fritz_sync_id_key, s",
			Usage: "`KEY` under which source IDs are being stored in the Fritz!Box",
		},
	}
	app.Action = func(ctx *cli.Context) error {
		boxURL := ctx.String("fritz_url")
		phonebookName := ctx.String("fritz_phonebook")
		fritzUser := ctx.String("fritz_user")
		fritzPass := ctx.String("fritz_password")
		syncIDKey := ctx.String("fritz_sync_id_key")

		ocABooks := ctx.StringSlice("carddav_url")
		ocUser := ctx.String("carddav_user")
		ocPass := ctx.String("carddav_password")

		if boxURL == "" {
			return errors.New("you have to specify the Fritz!Box URL")
		}
		if phonebookName == "" {
			return errors.New("you have to specify the Fritz!Box phonebook name")
		}
		if fritzUser == "" {
			return errors.New("you have to specify the Fritz!Box user")
		}
		if fritzPass == "" {
			return errors.New("you have to specify the Fritz!Box password")
		}
		if syncIDKey == "" {
			return errors.New("you have to specify the Fritz!Box sync ID key")
		}
		if len(ocABooks) == 0 {
			return errors.New("you have to specify at least one CardDAV addressbook URL")
		}
		if ocUser == "" {
			return errors.New("you have to specify the CardDAV user")
		}
		if ocPass == "" {
			return errors.New("you have to specify the CardDAV password")
		}

		fritzAdapter, err := fritzbox.NewAdapter(boxURL, phonebookName, fritzUser, fritzPass, syncIDKey)
		if err != nil {
			return err
		}
		var ocAdapters []sync.Reader
		for _, ocABook := range ocABooks {
			ocAdapters = append(ocAdapters, carddav.NewAdapter(ocABook, ocUser, ocPass))
		}

		return sync.Sync(ocAdapters, fritzAdapter, ctx.StringSlice("carddav_category"),
			log.New(os.Stdout, "", log.LstdFlags))
	}
	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}
