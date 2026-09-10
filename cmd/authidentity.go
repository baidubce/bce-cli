package cmd

import (
	"github.com/baidubce/bce-cli/internal/oauth"
)

// authIdentity bundles the non-sensitive identity fields read from a login's ID
// Token, shared by the text and JSON renderers of `bce auth login`/`status`.
type authIdentity struct {
	User          string
	AccountID     string
	PrincipalType string
}

// Identity is the single label used to refer to this login in prose (e.g. "already
// logged in as ..."): the account ID when known, else the username.
func (id authIdentity) Identity() string {
	if id.AccountID != "" {
		return id.AccountID
	}
	return id.User
}

func identityOf(ts *oauth.TokenSet) authIdentity {
	id := authIdentity{User: ts.Username}
	if claims, err := oauth.DecodeIDToken(ts.IDToken); err == nil {
		if id.User == "" {
			id.User = claims.Username
		}
		id.AccountID = claims.AccountID
		id.PrincipalType = claims.PrincipalType
	}
	return id
}

// authStatusJSONFields builds the field list shared by `bce auth login`'s
// success case and `bce auth status`'s logged-in case: they report the exact
// same session details.
func authStatusJSONFields(profile string, ts *oauth.TokenSet) []jsonField {
	id := identityOf(ts)
	fields := []jsonField{
		{"profile", profile},
		{"iam_base", ts.IAMBase},
		{"sts_base", ts.STSBase},
	}
	if id.User != "" {
		fields = append(fields, jsonField{"user", id.User})
	}
	if id.AccountID != "" {
		fields = append(fields, jsonField{"account_id", id.AccountID})
	}
	if id.PrincipalType != "" {
		fields = append(fields, jsonField{"principal_type", id.PrincipalType})
	}
	if ts.Scope != "" {
		fields = append(fields, jsonField{"scope", ts.Scope})
	}
	if s := formatExpiry(ts.IDTokenExpiresAt); s != "" {
		fields = append(fields, jsonField{"id_token_expires", s})
	}
	if s := formatExpiry(ts.RefreshTokenExpiresAt); s != "" {
		fields = append(fields, jsonField{"refresh_token_expires", s})
	}
	if ts.STS != nil {
		if s := formatExpiry(ts.STS.Expiration); s != "" {
			fields = append(fields, jsonField{"credentials_expire", s})
		}
	}
	return fields
}
