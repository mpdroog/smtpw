package main

import (
	"errors"
	"encoding/json"
	"encoding/base64"
	"fmt"
	"flag"
	"gopkg.in/gomail.v1"
	"github.com/mpdroog/beanstalkd" //"github.com/maxid/beanstalkd"
	"smtpw/config"
	"time"
	"strings"
	"os"
)

const ERR_WAIT_SEC = 5
var verbose bool
var readonly bool
var hostname string

func proc(m config.Email) error {
	conf, ok := config.C.From[m.From]
	if !ok {
		return errors.New("From does not exist: " + m.From)
	}

	msg := gomail.NewMessage()
	msg.SetHeader("Message-ID", fmt.Sprintf("<%s@%s>", RandText(32), hostname))
	msg.SetHeader("From", conf.Display + " <" + conf.From + ">")
	msg.SetHeader("To", m.To...)
	msg.SetHeader("Bcc", conf.Bcc...)
	msg.SetHeader("Subject", m.Subject)
	msg.SetBody("text/plain", m.Text)
	if len(m.Html) > 0 {
		msg.AddAlternative("text/html", m.Html)
	}

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

	if readonly {
		fmt.Println("From: " + conf.Display + " <" + conf.From + ">")
		fmt.Println(fmt.Sprintf("To: %v", m.To))
		fmt.Println(fmt.Sprintf("Bcc: %v", conf.Bcc))
		fmt.Println("Subject: " + m.Subject)
		fmt.Println("\ntext/plain")
		fmt.Println(m.Text)
		fmt.Println("\ntext/html")
		fmt.Println(m.Html)
		fmt.Println("\n")
		return nil
	}

	mailer := gomail.NewMailer(conf.Host, conf.User, conf.Pass, conf.Port)
	return mailer.Send(msg)
}

func connect() (*beanstalkd.BeanstalkdClient, error) {
	queue, e := beanstalkd.Dial(config.C.Beanstalk)
	if e != nil {
		return nil, e
	}
	// Only listen to email queue.
	queue.Use("email")
	if _, e := queue.Watch("email"); e != nil {
		return nil, e
	}
	queue.Ignore("default")
	return queue, nil
}

func main() {
	var (
		configPath string
		skipOne bool
	)
	flag.BoolVar(&verbose, "v", false, "Verbose-mode")
	flag.BoolVar(&skipOne, "s", false, "Delete e-mail on deverr")
	flag.BoolVar(&readonly, "r", false, "Don't email but flush to stdout")
	flag.StringVar(&configPath, "c", "./config.json", "Path to config.json")
	flag.Parse()

	if e := config.Init(configPath); e != nil {
		panic(e)
	}
	// TODO: Test config before starting?

	queue, e := connect()
	if e != nil {
		panic(e)
	}
	hostname, e = os.Hostname()
	if e != nil {
		panic(e)
	}

	if verbose {
		fmt.Println("SMTPw(" + hostname + ") email-tube (ignoring default)")
	}
	if readonly {
		fmt.Println("!! ReadOnly mode !!")
	}
	for {
		job, e := queue.Reserve(0)
		if e != nil {
			fmt.Println("Beanstalkd err: " + e.Error())
			time.Sleep(time.Second * ERR_WAIT_SEC)
			if strings.HasSuffix(e.Error(), "broken pipe") {
				// Beanstalkd down, reconnect!
				q, e := connect()
				if e != nil {
					fmt.Println("Reconnect err: " + e.Error())
				}
				if q != nil {
					queue = q
				}
			}
			continue
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
		if verbose {
			fmt.Println(fmt.Sprintf("Finished job %d", job.Id))
		}
	}
	queue.Quit()
}
