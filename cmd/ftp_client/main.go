package main

import (
	"crypto/tls"
	"fmt"
	"log"
	"net/url"
	"os"

	"github.com/jlaffaye/ftp"
	"github.com/urfave/cli"
)

func main() {
	app := cli.NewApp()
	app.Usage = "an FTP test app"
	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:  "server_url, s",
			Usage: "`URL` of the FTP server",
		},
		cli.StringFlag{
			Name:  "user, u",
			Usage: "`USERNAME` to connect to the FTP server",
		},
		cli.StringFlag{
			Name:  "password, p",
			Usage: "`PASSWORD` to connect to the FTP server",
		},
	}
	app.Action = func(ctx *cli.Context) error {
		ftpURL := ctx.String("server_url")
		user := ctx.String("user")
		pass := ctx.String("password")

		uri, err := url.Parse(ftpURL)
		if err != nil {
			return fmt.Errorf("cannot parse FTP URL: %v", err)
		}
		fmt.Println("uri:", uri, "host", uri.Hostname())
		ftpClient, err := ftp.Dial(uri.Hostname()+":21", ftp.DialWithExplicitTLS(&tls.Config{ServerName: uri.Hostname()}))
		if err != nil {
			return fmt.Errorf("cannot connect to FTP server: %v", err)
		}
		fmt.Println("connected")
		if err := ftpClient.Login(user, pass); err != nil {
			return fmt.Errorf("cannot log into FTP server: %v", err)
		}
		fmt.Println("logged in")
		return nil
	}
	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}
