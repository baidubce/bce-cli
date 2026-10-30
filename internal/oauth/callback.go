package oauth

import (
	"context"
	"crypto/hmac"
	"errors"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"sync"
	"time"
)

// DefaultCallbackTimeout matches the IAM authorization transaction lifetime.
const DefaultCallbackTimeout = 300 * time.Second

// ErrCallbackTimeout is returned by Wait when no callback arrives before the
// deadline. It is a sentinel so callers can classify it without parsing text.
var ErrCallbackTimeout = errors.New("timed out waiting for the browser callback")

// CallbackServer is a single-shot HTTP listener on a loopback port that
// receives the browser redirect of one authorization request.
type CallbackServer struct {
	listener    net.Listener
	redirectURI string
	state       string

	once   sync.Once
	result chan callbackResult
	srv    *http.Server
}

type callbackResult struct {
	code string
	err  error
}

// AuthorizationError reports that IAM redirected back with an OAuth error
// instead of an authorization code (e.g. the user denied the request).
type AuthorizationError struct {
	Code        string
	Description string
}

func (e *AuthorizationError) Error() string {
	if e.Description != "" {
		return fmt.Sprintf("authorization failed: %s: %s", e.Code, e.Description)
	}
	return fmt.Sprintf("authorization failed: %s", e.Code)
}

// NewCallbackServer binds a random loopback port and starts serving /callback.
// The port is bound before the authorization URL is built so the redirect_uri
// can never point at a port owned by another process.
func NewCallbackServer(state string) (*CallbackServer, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen on loopback port: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	cs := &CallbackServer{
		listener:    ln,
		redirectURI: fmt.Sprintf("http://127.0.0.1:%d/callback", port),
		state:       state,
		result:      make(chan callbackResult, 1),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", cs.handleCallback)
	cs.srv = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() { _ = cs.srv.Serve(ln) }()
	return cs, nil
}

// RedirectURI returns the exact redirect_uri string to use in both the
// authorization request and the subsequent token request.
func (cs *CallbackServer) RedirectURI() string { return cs.redirectURI }

// Wait blocks until the first valid callback arrives, the context is done, or
// the timeout elapses, then returns the authorization code.
func (cs *CallbackServer) Wait(ctx context.Context, timeout time.Duration) (string, error) {
	if timeout <= 0 {
		timeout = DefaultCallbackTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case r := <-cs.result:
		return r.code, r.err
	case <-timer.C:
		return "", fmt.Errorf("%w after %s", ErrCallbackTimeout, timeout)
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// Close shuts the listener down. Safe to call more than once.
func (cs *CallbackServer) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = cs.srv.Shutdown(ctx)
}

func (cs *CallbackServer) handleCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	if oauthErr := q.Get("error"); oauthErr != "" {
		if !cs.claim() {
			writeCallbackPage(w, pageInfo,
				"此登录回调已被使用", "This login callback has already been used",
				"请返回终端查看登录结果。", "Please return to the terminal for the login result.")
			return
		}
		writeCallbackPage(w, pageError,
			"登录失败", "Login Failed",
			"请在终端查看失败详情。", "Check the terminal for failure details.")
		cs.finish(callbackResult{err: &AuthorizationError{
			Code:        oauthErr,
			Description: q.Get("error_description"),
		}})
		return
	}

	// Validate state before consuming the single-use slot. The IAM contract
	// requires aborting the flow on a state mismatch, so this still terminates
	// the login; not consuming the slot keeps the "already used" response
	// reserved for genuine replays of a valid callback.
	gotState := q.Get("state")
	if gotState == "" || !hmac.Equal([]byte(gotState), []byte(cs.state)) {
		writeCallbackPage(w, pageError,
			"登录失败：状态校验不通过", "Login Failed: State Mismatch",
			"回调状态与登录请求不一致，已中止登录。", "The callback state does not match the login request; aborted.")
		cs.finish(callbackResult{err: errors.New("callback state does not match the login request; aborting")})
		return
	}

	// Only the first valid callback of this login flow is honoured; anything
	// after it (a reload, a replayed link) never reaches the token endpoint.
	if !cs.claim() {
		writeCallbackPage(w, pageInfo,
			"此登录回调已被使用", "This login callback has already been used",
			"请返回终端查看登录结果。", "Please return to the terminal for the login result.")
		return
	}

	code := q.Get("code")
	if code == "" {
		writeCallbackPage(w, pageError,
			"登录失败：缺少授权码", "Login Failed: Missing Authorization Code",
			"回调中未包含授权码，无法继续登录。", "The callback did not contain an authorization code.")
		cs.finish(callbackResult{err: errors.New("callback did not contain an authorization code")})
		return
	}

	writeCallbackPage(w, pageSuccess,
		"登录成功", "Login Succeeded",
		"您可以关闭此页面并返回终端。", "You can close this page and return to the terminal.")
	cs.finish(callbackResult{code: code})
}

// claim reserves the single-use callback slot, reporting false if it is taken.
func (cs *CallbackServer) claim() bool {
	first := false
	cs.once.Do(func() { first = true })
	return first
}

func (cs *CallbackServer) finish(r callbackResult) {
	select {
	case cs.result <- r:
	default:
	}
}

// pageKind identifies the outcome category of a callback page, driving both the
// HTTP status code and the visual treatment (icon + colour).
type pageKind int

const (
	pageSuccess pageKind = iota // 200 OK, green check icon
	pageError                   // 400 Bad Request, red cross icon
	pageInfo                    // 403 Forbidden, amber info icon
)

// callbackPageModel is the data passed to the bilingual callback HTML template.
// The IsSuccess/IsError/IsInfo methods let the template branch on outcome
// semantics rather than the underlying iota integer, so reordering the
// pageKind constants can never silently desync the icon from the result.
type callbackPageModel struct {
	Kind    pageKind
	TitleZh string
	TitleEn string
	MsgZh   string
	MsgEn   string
}

func (m callbackPageModel) IsSuccess() bool { return m.Kind == pageSuccess }
func (m callbackPageModel) IsError() bool   { return m.Kind == pageError }
func (m callbackPageModel) IsInfo() bool    { return m.Kind == pageInfo }

// callbackTmpl is a self-contained, dependency-free HTML template for the
// browser-facing OAuth callback page. It renders a centred card with an SVG
// icon whose colour/shape depends on the outcome, plus bilingual (zh-CN /
// en-US) title and message lines. All interpolated values are auto-escaped by
// html/template.
var callbackTmpl = template.Must(template.New("callback").Parse(`<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.TitleZh}} · {{.TitleEn}}</title>
<style>
  :root { color-scheme: light dark; }
  * { box-sizing: border-box; }
  html, body { height: 100%; margin: 0; }
  body {
    display: flex; align-items: center; justify-content: center;
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC",
                 "Hiragino Sans GB", "Microsoft YaHei", Roboto, Helvetica, Arial, sans-serif;
    background: #f5f6f8; color: #1f2329;
  }
  .card {
    width: min(440px, 92vw);
    background: #ffffff; border-radius: 16px;
    box-shadow: 0 8px 32px rgba(0,0,0,.08);
    padding: 40px 36px 32px; text-align: center;
  }
  .icon { width: 64px; height: 64px; margin: 0 auto 20px; display: block; }
  .title-zh { font-size: 22px; font-weight: 600; margin: 0 0 6px; line-height: 1.3; }
  .title-en { font-size: 15px; font-weight: 500; color: #646a73; margin: 0 0 22px; line-height: 1.3; }
  .msg-zh   { font-size: 15px; color: #1f2329; margin: 0 0 4px; line-height: 1.6; }
  .msg-en   { font-size: 13px; color: #8a9099; margin: 0 0 24px; line-height: 1.6; }
  .footer   { font-size: 12px; color: #bbbfc4; margin: 0; line-height: 1.6; }
  @media (prefers-color-scheme: dark) {
    body { background: #1d1f23; color: #e5e6eb; }
    .card { background: #2b2d31; box-shadow: 0 8px 32px rgba(0,0,0,.32); }
    .title-en { color: #a0a4ab; }
    .msg-zh { color: #e5e6eb; }
    .msg-en { color: #8b8f96; }
    .footer { color: #64666d; }
  }
</style>
</head>
<body>
  <main class="card">
    {{if .IsSuccess}}
      <svg class="icon" viewBox="0 0 52 52" fill="none" xmlns="http://www.w3.org/2000/svg">
        <circle cx="26" cy="26" r="25" fill="#e8f8ef"/>
        <path d="M16 27l7 7 13-14" stroke="#00b42a" stroke-width="3.5" stroke-linecap="round" stroke-linejoin="round"/>
      </svg>
    {{else if .IsError}}
      <svg class="icon" viewBox="0 0 52 52" fill="none" xmlns="http://www.w3.org/2000/svg">
        <circle cx="26" cy="26" r="25" fill="#ffece8"/>
        <path d="M18 18l16 16M34 18L18 34" stroke="#f53f3f" stroke-width="3.5" stroke-linecap="round"/>
      </svg>
    {{else}}
      <svg class="icon" viewBox="0 0 52 52" fill="none" xmlns="http://www.w3.org/2000/svg">
        <circle cx="26" cy="26" r="25" fill="#fff7e8"/>
        <path d="M26 15v13" stroke="#ff9900" stroke-width="3.5" stroke-linecap="round"/>
        <circle cx="26" cy="37" r="2.4" fill="#ff9900"/>
      </svg>
    {{end}}
    <p class="title-zh">{{.TitleZh}}</p>
    <p class="title-en">{{.TitleEn}}</p>
    <p class="msg-zh">{{.MsgZh}}</p>
    <p class="msg-en">{{.MsgEn}}</p>
    <p class="footer">BCE CLI · 百度云命令行工具</p>
  </main>
</body>
</html>`))

func writeCallbackPage(w http.ResponseWriter, kind pageKind, titleZh, titleEn, msgZh, msgEn string) {
	status := http.StatusBadRequest
	switch kind {
	case pageSuccess:
		status = http.StatusOK
	case pageInfo:
		status = http.StatusForbidden
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	// The template is parsed once at init and only interpolates string fields,
	// so Execute cannot fail in practice; the header is already sent, so there
	// is nothing actionable to do with an error here.
	_ = callbackTmpl.Execute(w, callbackPageModel{
		Kind:    kind,
		TitleZh: titleZh,
		TitleEn: titleEn,
		MsgZh:   msgZh,
		MsgEn:   msgEn,
	})
}
