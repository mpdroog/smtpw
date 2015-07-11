package main

import (
	"errors"
	"encoding/json"
	"encoding/base64"
	"fmt"
	"flag"
	"gopkg.in/gomail.v1"
	"github.com/maxid/beanstalkd"
	"smtpw/config"
	"time"
	"strings"
)

const ERR_WAIT_SEC = 5
var verbose bool

func proc(m config.Email) error {
	conf, ok := config.C.From[m.From]
	if !ok {
		return errors.New("From does not exist: " + m.From)
	}

	msg := gomail.NewMessage()
	msg.SetHeader("From", conf.Display)
	msg.SetHeader("To", m.To...)
	msg.SetHeader("Bcc", conf.Bcc...)
	msg.SetHeader("Subject", m.Subject)
	msg.SetBody("text/plain", m.Text)
	msg.SetBody("text/html", m.Html)

	for name, embed := range m.HtmlEmbed {
		raw, e := base64.StdEncoding.DecodeString(embed)
		if e != nil {
			return errors.New("HtmlEmbed: " + name + " is not base64!")
		}
		if !strings.Contains(m.Html, fmt.Sprintf("cid:" + name)) {
			return errors.New("HtmlEmbed: " + name + " is not used in the HTML!")
		}
		msg.Embed(gomail.CreateFile(name, raw))
	}

	mailer := gomail.NewMailer(conf.Host, conf.User, conf.Pass, conf.Port)
	return mailer.Send(msg)
}

func main() {
	var (
		configPath string
		skipOne bool
	)
	flag.BoolVar(&verbose, "v", false, "Verbose-mode")
	flag.BoolVar(&skipOne, "s", false, "Delete e-mail on deverr")
	flag.StringVar(&configPath, "c", "./config.json", "Path to config.json")
	flag.Parse()

	if e := config.Init(configPath); e != nil {
		panic(e)
	}
	// TODO: Test config before starting?

	queue, e := beanstalkd.Dial(config.C.Beanstalk)
	if e != nil {
		panic(e)
	}

	for {
		job, e := queue.Reserve(0)
		if e != nil {
			fmt.Println("Beanstalkd err:" + e.Error())
			time.Sleep(time.Second * ERR_WAIT_SEC)
		}
		if verbose {
			fmt.Println(fmt.Sprintf("Parse job %d", job.Id))
			fmt.Println("JSON:\r\n" + string(job.Data))
		}
		// Parse
		var m config.Email
		if e := json.Unmarshal(job.Data, &m); e != nil {
			// Broken JSON
			if skipOne {
				fmt.Println("WARN: Skip job as JSON is invalid")
				queue.Delete(job.Id)
				skipOne = false
				continue
			}

			// Ignore decode trouble
			fmt.Println("CRIT: Invalid JSON received (msg=" + e.Error() + ")")
			continue
		}

		if e := proc(m); e != nil {
			// TODO: Isolate deverr from senderr
			// Processing trouble?
			fmt.Println("WARN: Failed sending, retry in 20sec (msg=" + e.Error() + ")")			
			continue
		}
		queue.Delete(job.Id)
	}
	queue.Quit()
}