package tgclient

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
)

// ErrNotAuthorized is returned by EnsureAuthorized when there is no valid saved
// session. The caller should tell the user to run `tgmcp login` first.
var ErrNotAuthorized = errors.New("not authorized: run `tgmcp login` once to create a session")

// EnsureAuthorized verifies a usable session exists, without prompting.
func (c *Client) EnsureAuthorized(ctx context.Context) error {
	status, err := c.tg.Auth().Status(ctx)
	if err != nil {
		return fmt.Errorf("auth status: %w", err)
	}
	if !status.Authorized {
		return ErrNotAuthorized
	}
	if u := status.User; u != nil {
		c.log.Sugar().Infof("authorized as %s (@%s)", u.FirstName, u.Username)
	}
	return nil
}

// Login runs the interactive auth flow (phone -> code -> optional 2FA) and
// persists the session. Intended to be called from a terminal, not over stdio.
func (c *Client) Login(ctx context.Context) error {
	flow := auth.NewFlow(
		termAuth{phone: c.cfg.Phone, password: c.cfg.Password},
		auth.SendCodeOptions{},
	)
	if err := c.tg.Auth().IfNecessary(ctx, flow); err != nil {
		return err
	}
	self, err := c.tg.Self(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Logged in as %s %s (@%s)\n", self.FirstName, self.LastName, self.Username)
	return nil
}

// termAuth implements auth.UserAuthenticator, reading from the terminal whatever
// was not pre-supplied via env (TG_PHONE / TG_PASSWORD).
type termAuth struct {
	phone    string
	password string
}

func (a termAuth) Phone(_ context.Context) (string, error) {
	if a.phone != "" {
		return a.phone, nil
	}
	return prompt("Phone (international, e.g. +1234567890): ")
}

func (a termAuth) Password(_ context.Context) (string, error) {
	if a.password != "" {
		return a.password, nil
	}
	return prompt("2FA password: ")
}

func (a termAuth) Code(_ context.Context, _ *tg.AuthSentCode) (string, error) {
	return prompt("Code (from Telegram): ")
}

func (a termAuth) AcceptTermsOfService(_ context.Context, _ tg.HelpTermsOfService) error {
	return nil
}

func (a termAuth) SignUp(_ context.Context) (auth.UserInfo, error) {
	return auth.UserInfo{}, errors.New("sign up is not supported; use an existing Telegram account")
}

func prompt(label string) (string, error) {
	fmt.Fprint(os.Stderr, label)
	s, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(s), nil
}
