package routing

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"time"

	"github.com/element-hq/dendrite/setup/config"
)

const smtpDefaultTimeout = 30 * time.Second

func sendEmailViaSMTP(ctx context.Context, smtpCfg *config.SMTP, fromAddr, toAddr *mail.Address, message string) error {
	addr := fmt.Sprintf("%s:%d", smtpCfg.Host, smtpCfg.Port)
	dialer := &net.Dialer{Timeout: smtpDefaultTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to dial SMTP server: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(smtpDefaultTimeout))

	client, err := smtp.NewClient(conn, smtpCfg.Host)
	if err != nil {
		return fmt.Errorf("failed to create SMTP client: %w", err)
	}
	defer client.Quit()

	tlsActive := false
	if smtpCfg.RequireTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return fmt.Errorf("smtp server %s does not support STARTTLS", smtpCfg.Host)
		}
		tlsConfig := &tls.Config{ServerName: smtpCfg.Host}
		if err := client.StartTLS(tlsConfig); err != nil {
			return fmt.Errorf("failed to start TLS: %w", err)
		}
		tlsActive = true
	} else if ok, _ := client.Extension("STARTTLS"); ok {
		tlsConfig := &tls.Config{ServerName: smtpCfg.Host}
		if err := client.StartTLS(tlsConfig); err != nil {
			return fmt.Errorf("failed to start optional TLS: %w", err)
		}
		tlsActive = true
	}

	if smtpCfg.Username != "" {
		password := smtpCfg.GetPassword()
		if password == "" {
			return fmt.Errorf("smtp password not configured")
		}
		if !tlsActive {
			return fmt.Errorf("smtp auth refused without TLS")
		}
		auth := smtp.PlainAuth("", smtpCfg.Username, password, smtpCfg.Host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth failed: %w", err)
		}
	}

	if err := client.Mail(fromAddr.Address); err != nil {
		return fmt.Errorf("smtp mail failed: %w", err)
	}
	if err := client.Rcpt(toAddr.Address); err != nil {
		return fmt.Errorf("smtp rcpt failed: %w", err)
	}

	wc, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp data failed: %w", err)
	}
	if _, err := wc.Write([]byte(message)); err != nil {
		_ = wc.Close()
		return fmt.Errorf("smtp write failed: %w", err)
	}
	if err := wc.Close(); err != nil {
		return fmt.Errorf("smtp data close failed: %w", err)
	}

	return client.Quit()
}
