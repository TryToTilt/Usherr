package notify

// Usherr: emails that aren't notifications (a link to set a new password)
// go through the mail server of the first email notification that is
// switched on, to one address only, never to the notification's own
// recipients.

import (
	"context"
	"errors"
)

// AccountMailer is the mail server for account emails, with the name of the
// email notification it belongs to; ok is false when no email notification
// is switched on (or its password can't be read).
func (r *Repo) AccountMailer() (sender *EmailSender, name string, ok bool) {
	targets, err := r.List()
	if err != nil {
		return nil, "", false
	}
	for _, t := range targets {
		if !t.Enabled || t.Type != TargetEmail || t.SecretUnreadable {
			continue
		}
		s, err := NewSender(t)
		if err != nil {
			continue
		}
		if es, isEmail := s.(*EmailSender); isEmail && es.Host != "" && es.From != "" {
			return es, t.Name, true
		}
	}
	return nil, "", false
}

// SendTo sends a plain-text email to one address through this mail server.
func (s *EmailSender) SendTo(ctx context.Context, to string, ev Event) error {
	if to == "" {
		return errors.New("no address to send to")
	}
	one := *s
	one.To = []string{to}
	ev.Lead = "" // plain text: no poster, table or buttons
	return one.Send(ctx, ev)
}
