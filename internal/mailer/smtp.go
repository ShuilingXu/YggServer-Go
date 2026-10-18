package mailer

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"
)

type SMTP struct{ Addr, Mode, User, Password, From string }

func (s SMTP) Send(ctx context.Context, recipient, subject, body string) error {
	from, err := mail.ParseAddress(s.From)
	if err != nil || strings.ContainsAny(recipient+subject, "\r\n") {
		return fmt.Errorf("invalid mail message")
	}
	host, _, err := net.SplitHostPort(s.Addr)
	if err != nil {
		return fmt.Errorf("invalid mail configuration")
	}
	dialer := net.Dialer{Timeout: 5 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", s.Addr)
	if err != nil {
		return fmt.Errorf("mail transport unavailable")
	}
	defer conn.Close()
	deadline := time.Now().Add(10 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetDeadline(deadline)
	stopped := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stopped()
	tlsConfig := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	if s.Mode == "tls" {
		encrypted := tls.Client(conn, tlsConfig)
		if err := encrypted.HandshakeContext(ctx); err != nil {
			return fmt.Errorf("mail tls unavailable")
		}
		conn = encrypted
	}
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return fmt.Errorf("mail transport unavailable")
	}
	defer client.Close()
	if s.Mode == "starttls" {
		if err := client.StartTLS(tlsConfig); err != nil {
			return fmt.Errorf("mail tls unavailable")
		}
	}
	if s.User != "" {
		if err := client.Auth(smtp.PlainAuth("", s.User, s.Password, host)); err != nil {
			return fmt.Errorf("mail authentication failed")
		}
	}
	if err := client.Mail(from.Address); err != nil {
		return fmt.Errorf("mail sender rejected")
	}
	if err := client.Rcpt(recipient); err != nil {
		return fmt.Errorf("mail recipient rejected")
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("mail delivery unavailable")
	}
	_, writeErr := fmt.Fprintf(writer, "From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s\r\n", s.From, recipient, subject, strings.ReplaceAll(body, "\n", "\r\n"))
	closeErr := writer.Close()
	if writeErr != nil || closeErr != nil {
		return fmt.Errorf("mail delivery failed")
	}
	// DATA 成功代表已交给 SMTP；QUIT 失败不能使用户重复请求同一封邮件。
	_ = client.Quit()
	return nil
}
