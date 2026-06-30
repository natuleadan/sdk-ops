package notify

import (
	"fmt"
	"net/smtp"
	"strings"
)

type SMTP struct {
	host     string
	port     int
	user     string
	pass     string
	from     string
	to       []string
}

func NewSMTP(host string, port int, user, pass, from string, to []string) *SMTP {
	if port == 0 {
		port = 587
	}
	return &SMTP{host: host, port: port, user: user, pass: pass, from: from, to: to}
}

func (s *SMTP) Send(title, message string) error {
	addr := fmt.Sprintf("%s:%d", s.host, s.port)
	subject := title
	if subject == "" {
		subject = "sdk-ops notification"
	}
	body := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		s.from, strings.Join(s.to, ","), subject, message)

	auth := smtp.PlainAuth("", s.user, s.pass, s.host)
	if err := smtp.SendMail(addr, auth, s.from, s.to, []byte(body)); err != nil {
		return fmt.Errorf("smtp send: %w", err)
	}
	return nil
}
