package handlers

import (
	"crypto/sha256"
	"encoding/base64"
	"html/template"
	"net/http"

	"github.com/gin-gonic/gin"
)

const authPageScript = `const form=document.querySelector('form');form.addEventListener('submit',async(event)=>{event.preventDefault();const output=document.querySelector('output');const body={token:form.elements.token.value};if(form.elements.password)body.password=form.elements.password.value;try{const response=await fetch(form.action,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)});const result=await response.json();output.textContent=result.message||result.error;if(response.ok)form.querySelector('button').disabled=true;}catch(error){output.textContent='Could not connect. Please try again.';}});`

const authPageHTML = `<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>{{.Title}} · MeppVP</title><main><h1>{{.Title}}</h1><form action="{{.Endpoint}}" method="post"><input type="hidden" name="token" value="{{.Token}}">{{if .Reset}}<label>New password (8–1024 characters, uppercase, lowercase and a number)<input type="password" name="password" minlength="8" maxlength="1024" autocomplete="new-password" required></label>{{end}}<button type="submit">{{.Title}}</button></form><output aria-live="polite"></output><p>Once complete, return to the MeppVP app.</p></main><script>` + authPageScript + `</script></html>`

// AuthLinkPage renders a confirmation form; opening a mail link never consumes
// the token, so email scanners cannot verify or reset an account accidentally.
func AuthLinkPage(reset bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.Query("token")
		if token == "" || len(token) > 128 {
			ErrorJSON(c, http.StatusBadRequest, "invalid token")
			return
		}
		page, err := template.New("auth").Parse(authPageHTML)
		if err != nil {
			ErrorJSON(c, http.StatusInternalServerError, "could not render form")
			return
		}
		title, endpoint := "Verify email", "/api/auth/verify-email"
		if reset {
			title, endpoint = "Reset password", "/api/auth/reset-password"
		}
		hash := sha256.Sum256([]byte(authPageScript))
		c.Header("Content-Security-Policy", "default-src 'none'; script-src 'sha256-"+base64.StdEncoding.EncodeToString(hash[:])+"'; connect-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("Cache-Control", "no-store")
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.Status(http.StatusOK)
		if err := page.Execute(c.Writer, struct {
			Title, Endpoint, Token string
			Reset                  bool
		}{title, endpoint, token, reset}); err != nil {
			_ = c.Error(err)
		}
	}
}
