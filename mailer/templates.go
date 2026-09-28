package mailer

import (
	"bytes"
	"embed"
	"fmt"
	htmltemplate "html/template"
	texttemplate "text/template"
)

//go:embed templates/*.tmpl
var templateFS embed.FS

var (
	textTemplates = texttemplate.Must(texttemplate.ParseFS(templateFS, "templates/*.txt.tmpl"))
	htmlTemplates = htmltemplate.Must(htmltemplate.ParseFS(templateFS, "templates/*.html.tmpl"))
)

// TemplateData is what every email template receives.
type TemplateData struct {
	AppName   string // product name, from the sender's display name
	Name      string // recipient's display name; templates fall back to "there"
	Link      string // absolute URL the email asks the reader to open
	ExpiresIn string // human-readable lifetime of the link
}

// render executes the text and HTML variants of the named template. name is
// the file stem, e.g. "verify_email".
func render(name string, data TemplateData) (text, html string, err error) {
	var tb, hb bytes.Buffer
	if err := textTemplates.ExecuteTemplate(&tb, name+".txt.tmpl", data); err != nil {
		return "", "", fmt.Errorf("render %s text: %w", name, err)
	}
	if err := htmlTemplates.ExecuteTemplate(&hb, name+".html.tmpl", data); err != nil {
		return "", "", fmt.Errorf("render %s html: %w", name, err)
	}
	return tb.String(), hb.String(), nil
}

// VerificationMessage builds the email that asks a new user to confirm their
// address. link must be absolute.
func VerificationMessage(appName, to, name, link string) (Message, error) {
	text, html, err := render("verify_email", TemplateData{AppName: appName, Name: name, Link: link, ExpiresIn: "24 hours"})
	if err != nil {
		return Message{}, err
	}
	return Message{To: to, Subject: fmt.Sprintf("Verify your email for %s", appName), Text: text, HTML: html}, nil
}

// PasswordResetMessage builds the email carrying a password reset link.
func PasswordResetMessage(appName, to, name, link string) (Message, error) {
	text, html, err := render("password_reset", TemplateData{AppName: appName, Name: name, Link: link, ExpiresIn: "1 hour"})
	if err != nil {
		return Message{}, err
	}
	return Message{To: to, Subject: fmt.Sprintf("Reset your %s password", appName), Text: text, HTML: html}, nil
}
