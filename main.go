package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/mpdroog/beanstalkd" //"github.com/maxid/beanstalkd"
	"gopkg.in/gomail.v1"
	"log"
	"os"
	"github.com/mpdroog/smtpw/config"
	"strings"
	"time"
	"github.com/coreos/go-systemd/daemon"
)

const ERR_WAIT_SEC = 5

var errTimedOut = errors.New("timed out")

var verbose bool
var readonly bool
var hostname string
var L *log.Logger

func proc(m config.Email, skipOne bool) error {
	conf, ok := config.C.From[m.From]
	if !ok {
		return errors.New("From does not exist: " + m.From)
	}

	host := conf.Hostname
	if host == "" {
		host = hostname
	}

	msg := gomail.NewMessage()
	msg.SetHeader("Message-ID", fmt.Sprintf("<%s@%s>", RandText(32), host))
	msg.SetHeader("X-Mailer", "smtpw")
	msg.SetHeader("X-Priority", "3")
	if conf.Bounce == nil {
		msg.SetHeader("From", conf.Display+" <"+conf.From+">")
	} else {
		// Set bounce handling
		// From receives bounces
		// Human-clients send to Reply-To
		msg.SetHeader("From", fmt.Sprintf("%s <%s>", conf.Display, *conf.Bounce))
		msg.SetHeader("Reply-To", fmt.Sprintf("%s <%s>", conf.Display, conf.From))
	}
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
			if skipOne {
				L.Printf("WARN: HtmlEmbed: " + name + " is not base64!\n")
				return nil
			}
			return errors.New("HtmlEmbed: " + name + " is not base64!")
		}
		if !strings.Contains(m.Html, fmt.Sprintf("cid:"+name)) {
			if skipOne {
				L.Printf("WARN: HtmlEmbed: " + name + " is not used in the HTML!")
				return nil
			}
			return errors.New("HtmlEmbed: " + name + " is not used in the HTML!")
		}
		msg.Embed(gomail.CreateFile(name, raw))
	}
	for name, attachment := range m.Attachments {
		raw, e := base64.StdEncoding.DecodeString(attachment)
		if e != nil {
			if skipOne {
				L.Printf("WARN: Attachment: " + name + " is not base64!")
				return nil
			}
			return errors.New("Attachment: " + name + " is not base64!")
		}
		msg.Embed(gomail.CreateFile(name, raw))
	}

	if readonly {
		L.Printf("From: %s <%s>\n", conf.Display, conf.From)
		L.Printf("To: %v\n", m.To)
		L.Printf("Bcc: %v\n", conf.Bcc)
		L.Printf("Subject: %s\n", m.Subject)
		L.Printf("\ntext/plain\n")
		L.Printf(m.Text)
		L.Printf("\ntext/html\n")
		L.Printf(m.Html)
		L.Printf("\n\n")
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
		skipOne    bool
	)
	L = log.New(os.Stdout, "", log.LstdFlags)

	flag.BoolVar(&verbose, "v", false, "Verbose-mode")
	flag.BoolVar(&skipOne, "s", false, "Delete e-mail on deverr")
	flag.BoolVar(&readonly, "r", false, "Don't email but flush to stdout")
	flag.StringVar(&configPath, "c", "./config.json", "Path to config.json")
	flag.Parse()

	if e := config.Init(configPath); e != nil {
		panic(e)
	}
	if verbose {
		L.Printf("%+v\n", config.C)
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
		L.Printf("SMTPw(%s) email-tube (ignoring default)\n", hostname)
	}
	if readonly {
		L.Printf("!! ReadOnly mode !!\n")
	}

	sent, e := daemon.SdNotify(false, "READY=1")
	if e != nil {
		panic(e)
	}
	if !sent {
		L.Printf("SystemD notify NOT sent\n")
        }

	for {
		job, e := queue.Reserve(15 * 60) //15min timeout
		if e != nil {
			if e.Error() == errTimedOut.Error() {
				// Reserve timeout
				continue
			}

			L.Printf("Beanstalkd err: %s\n", e.Error())
			time.Sleep(time.Second * ERR_WAIT_SEC)
			if strings.HasSuffix(e.Error(), "broken pipe") {
				// Beanstalkd down, reconnect!
				q, e := connect()
				if e != nil {
					L.Printf("Reconnect err: %s\n", e.Error())
				}
				if q != nil {
					queue = q
				}
			}
			continue
		}
		if verbose {
			L.Printf("Parse job %d\n", job.Id)
			L.Printf("JSON:\n%s\n", string(job.Data))
		}
		// Parse
		var m config.Email
		if e := json.Unmarshal(job.Data, &m); e != nil {
			// Broken JSON
			if skipOne {
				L.Printf("WARN: Skip job as JSON is invalid\n")
				queue.Delete(job.Id)
				skipOne = false
				continue
			}

			// Ignore decode trouble
			L.Printf("CRIT: Invalid JSON received (msg=%s)\n", e.Error())
			continue
		}

		if e := proc(m, skipOne); e != nil {
			// 501 Syntax error in parameters or arguments
			// http://www.greenend.org.uk/rjk/tech/smtpreplies.html
			if strings.HasPrefix(e.Error(), "501 ") {
				L.Printf("WARN: Job buried, invalid email address(es)? (msg=%s)\n", e.Error())
				queue.Bury(job.Id, 1)
				continue
			}
			// TODO: Isolate deverr from senderr
			// Processing trouble?
			L.Printf("WARN: Failed sending, retry in 20sec (msg=%s)\n", e.Error())
			time.Sleep(time.Second * 20)
			continue
		}
		queue.Delete(job.Id)
		if verbose {
			L.Printf("Finished job %d", job.Id)
		}
	}
	queue.Quit()
}
