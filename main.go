package main

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/mail"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/coreos/go-systemd/daemon"
	"github.com/mpdroog/beanstalkd" //"github.com/maxid/beanstalkd"
	"github.com/mpdroog/smtpw/config"
	"gopkg.in/gomail.v2"
)

const ERR_WAIT_SEC = 5

var errTimedOut = errors.New("timed out")

var verbose bool
var readonly bool
var debug bool
var hostname string
var L *log.Logger

// safeFilenameRe matches only safe characters for filenames
var safeFilenameRe = regexp.MustCompile(`[^a-zA-Z0-9._-]`)

// sanitizeFilename removes path components and unsafe characters from filename
func sanitizeFilename(name string) string {
	// Remove any path components
	name = filepath.Base(name)
	// Replace unsafe characters with underscore
	name = safeFilenameRe.ReplaceAllString(name, "_")
	// Limit length
	if len(name) > 255 {
		name = name[:255]
	}
	// Ensure non-empty
	if name == "" || name == "." || name == ".." {
		name = "attachment"
	}
	return name
}

// validateEmail checks size limits to prevent DoS
func validateEmail(m *config.Email) error {
	// Check body sizes
	if len(m.Text) > config.MaxBodySize {
		return fmt.Errorf("text body exceeds max size (%d > %d)", len(m.Text), config.MaxBodySize)
	}
	if len(m.Html) > config.MaxBodySize {
		return fmt.Errorf("html body exceeds max size (%d > %d)", len(m.Html), config.MaxBodySize)
	}

	// Check recipient count
	totalRecipients := len(m.To) + len(m.BCC)
	if totalRecipients > config.MaxRecipients {
		return fmt.Errorf("too many recipients (%d > %d)", totalRecipients, config.MaxRecipients)
	}

	// Check attachment count
	totalAttachments := len(m.Attachments) + len(m.HtmlEmbed)
	if totalAttachments > config.MaxAttachments {
		return fmt.Errorf("too many attachments (%d > %d)", totalAttachments, config.MaxAttachments)
	}

	// Check individual attachment sizes (base64 encoded, so actual size is ~75% of encoded)
	for name, data := range m.Attachments {
		if len(data) > config.MaxAttachmentSize {
			return fmt.Errorf("attachment %q exceeds max size (%d > %d)", name, len(data), config.MaxAttachmentSize)
		}
	}
	for name, data := range m.HtmlEmbed {
		if len(data) > config.MaxAttachmentSize {
			return fmt.Errorf("embed %q exceeds max size (%d > %d)", name, len(data), config.MaxAttachmentSize)
		}
	}

	return nil
}

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

	bcc := conf.Bcc
	if conf.AllowBCC {
		bcc = append(bcc, m.BCC...)
	}

	msg.SetHeader("To", m.To...)
	msg.SetHeader("Bcc", bcc...)
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
		safeName := sanitizeFilename(name)
		msg.Embed(safeName, gomail.SetCopyFunc(func(w io.Writer) error {
			_, err := w.Write(raw)
			return err
		}))
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
		safeName := sanitizeFilename(name)
		msg.Attach(safeName, gomail.SetCopyFunc(func(w io.Writer) error {
			_, err := w.Write(raw)
			return err
		}))
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

	dialer := gomail.NewDialer(conf.Host, conf.Port, conf.User, conf.Pass)
	dialer.TLSConfig = &tls.Config{ServerName: conf.Host}
	if conf.Insecure {
		dialer.TLSConfig = &tls.Config{InsecureSkipVerify: true}
	}
	dialer.Auth = LoginAuth(conf.User, conf.Pass)
	return dialer.DialAndSend(msg)
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
	flag.BoolVar(&debug, "d", false, "Debug-mode")
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

	// Setup graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	running := true
	go func() {
		sig := <-sigChan
		L.Printf("Received signal %v, shutting down gracefully...\n", sig)
		running = false
		// Close queue to interrupt blocking Reserve() call
		queue.Quit()
	}()

	for running {
		job, e := queue.Reserve(15 * 60) //15min timeout
		if e != nil {
			if !running {
				// Shutdown in progress, exit loop
				break
			}
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

		// Hackfix. Get rid of array when empty to prevent parsing issues here
		if bytes.Contains(job.Data, []byte(`,"attachments":[]`)) {
			job.Data = bytes.Replace(job.Data, []byte(`,"attachments":[]`), []byte(``), 1)
		}

		if debug {
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

		// Validate size limits
		if e := validateEmail(&m); e != nil {
			L.Printf("WARN: Job buried, validation failed (msg=%s)\n", e.Error())
			queue.Bury(job.Id, 1)
			continue
		}

		if verbose {
			L.Printf("Email (job=%d email=%s subject=%s)\n", job.Id, m.To[0], m.Subject)
		}

		ok := true
		for _, addr := range m.To {
			if _, e := mail.ParseAddress(addr); e != nil {
				L.Printf("WARN: Job buried, invalid email=%s (msg=%s)\n", addr, e.Error())
				ok = false
				break
			}
		}
		if !ok {
			// email(s) invalid, bury job
			queue.Bury(job.Id, 1)
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

	L.Printf("Closing queue connection...\n")
	queue.Quit()
	L.Printf("Shutdown complete.\n")
}
