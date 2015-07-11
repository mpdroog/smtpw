SMTP Worker
=============
Send queued e-mail in separate process through
a JSON abstraction.

Why?
=============
* Security, SMTP credentials are isolated from the website
* Simplicity, the website creates JSON and all SMTP logic is isolated here
* Scalable, things need to go faster? Start a second..third..fourth.. worker!
* Cleaner, no more ugly timeouts in the browser if trouble and no more lost emails/customers!

How?
=============
* Use Beanstalkd (http://kr.github.io/beanstalkd/) to add jobs in a queue.
* One or more SMTPw-instances read the queue and try to send
* Failure? Wait 20sec and try again!

Config
=============
```
{
	"beanstalk": "127.0.0.1:11300",             // Hostname:port to Beanstalkd
	"from": {
		"support": {                            // From in JSON matches to this from
			"user": "usr",                      // SMTP username
			"pass": "ps",                       // SMTP password
			"host": "smtp.itshosted.nl",        // SMTP hostname
			"port": 113,                        // SMTP port
			"from": "support@itshosted.nl",     // From-address (used in mail header)
			"display": "Usenet.Farm Support",   // Display-name added before From-address
			"bcc": [
				"mpdroog@icloud.com"            // Send a secret copy (for your own administration)
			]
		}
	}
}
```

Usage
=============
```
./smtpw -h
Usage of ./smtpw:
  -c="./config.json": Path to config.json
  -s=false: Delete e-mail on deverr
  -v=false: Verbose-mode
```

* Path config.json, where to find the config.json file
* Delete on deverr, delete and flush the e-mail content if
 JSON is invalid.
* Verbose-mode, log what we're doing

JSON
=============
```
type Email struct {
	From string                 // Key that MUST match From in config
	To []string
	Subject string
	Html string
	Text string
	HtmlEmbed map[string]string // file.png => base64(bytes)
}
```
WARN: I like my parsers strict. Meaning if you add entries to
HtmlEmbed I search for cid:file.png and if not found it will not
send the email!

Errors
=============
`panic: dial tcp 127.0.0.1:11300: connection refused`
Install Beanstalkd and update config if Beanstalkd is running elsewhere?

`x509: certificate signed by unknown authority`
Your SMTP-server has a self-signed certificate? Time to get
a signed one!

External deps
=============
* https://github.com/go-gomail/gomail
* https://github.com/maxid/beanstalkd
