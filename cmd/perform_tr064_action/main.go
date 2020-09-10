package main

import (
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/huin/goupnp/soap"
	"github.com/urfave/cli"

	"github.com/toaster/fritz_sync/tr064"
)

func main() {
	app := cli.NewApp()
	app.Usage = "perform generic TR064 actions"
	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:  "base_url, b",
			Usage: "`URL` of the TR064 provider",
		},
		cli.StringFlag{
			Name:  "control_url, c",
			Usage: "service control `URL`",
		},
		cli.StringFlag{
			Name:  "user, u",
			Usage: "`USERNAME`",
		},
		cli.StringFlag{
			Name:  "password, p",
			Usage: "`PASSWORD`",
		},
		cli.StringFlag{
			Name:  "namespace, n",
			Usage: "`NAMESPACE`",
		},
		cli.StringFlag{
			Name:  "action, a",
			Usage: "`ACTION`",
		},
		cli.StringSliceFlag{
			Name:  "params, x",
			Usage: "`PARAMS` is a key/value list of action parameters",
		},
	}
	app.Action = func(ctx *cli.Context) error {
		baseURL := ctx.String("base_url")
		ctrlURL := ctx.String("control_url")
		user := ctx.String("user")
		pass := ctx.String("password")
		action := ctx.String("action")
		ns := ctx.String("namespace")
		paramPairs := ctx.StringSlice("params")

		if baseURL == "" {
			return errors.New("you have to specify the base URL")
		}
		if ctrlURL == "" {
			return errors.New("you have to specify the control URL")
		}
		if user == "" {
			return errors.New("you have to specify the user")
		}
		if pass == "" {
			return errors.New("you have to specify the password")
		}
		if action == "" {
			return errors.New("you have to specify the action")
		}

		adapter, err := tr064.NewAdapter(baseURL, ctrlURL, user, pass)
		if err != nil {
			return err
		}

		params := map[string]string{}
		for i := 0; i+1 < len(paramPairs); i += 2 {
			params[paramPairs[i]] = paramPairs[i+1]
		}
		result := tr064.UnknownXML{}
		if err := adapter.Perform(ns, action, params, &result); err != nil {
			if serr, ok := err.(*soap.SOAPFaultError); ok {
				fmt.Println("oops", serr.FaultCode, serr.FaultString, string(serr.Detail.Raw))
			}
			return err
		}
		fmt.Println("Output:", result)
		return nil
	}
	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}
