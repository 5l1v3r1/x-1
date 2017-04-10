package url

import (
	"net/url"
	"strings"
)

const (
	scrubbedValue       = "[SCRUBBED]"
	authHeaderLowerCase = "authorization"
)

var (
	// copy from https://github.com/heroku/rollbar-blanket/blob/master/lib/rollbar/blanket/fields.rb
	restrictedParams = map[string]bool{
		"access_token":                                true,
		"api_key":                                     true,
		"authenticity_token":                          true,
		"body.trace_chain.0.extra.cookies":            true,
		"body.trace_chain.0.extra.msg":                true,
		"body.trace_chain.0.extra.session.csrf.token": true,
		"bouncer.refresh_token":                       true,
		"bouncer.token":                               true,
		"confirm_password":                            true,
		"fingerprint":                                 true,
		"heroku_oauth_token":                          true,
		"heroku_session_nonce":                        true,
		"heroku_user_session":                         true,
		"key":                                true,
		"oauth_token":                        true,
		"old_secret":                         true,
		"passwd":                             true,
		"password":                           true,
		"password_confirmation":              true,
		"postgres_session_nonce":             true,
		"private_key":                        true,
		"request.cookies":                    true,
		"request.cookies.signup-sso-session": true,
		"request.params._csrf":               true,
		"request.session._csrf_token":        true,
		"request.session.csrf.token":         true,
		"secret":                             true,
		"secret_token":                       true,
		"sudo_oauth_token":                   true,
		"super_user_session_secret":          true,
		"token":                              true,
		"user_session_secret":                true,
		"www-sso-session":                    true,
	}
)

func URL(u *url.URL) *url.URL {
	// copy the url
	uu := *u
	query := uu.Query()
	for k, v := range query {
		if _, contains := restrictedParams[strings.ToLower(k)]; contains {
			query.Set(k, scrubbedValue)
			continue
		}

		// scrub values that are URLs
		for _, vv := range v {
			u, err := url.Parse(vv)
			if err != nil {
				continue
			}

			u.User = scrubURLUserInfo(u.User)
			query.Set(k, u.String())
		}
	}

	uu.RawQuery = query.Encode()
	uu.User = scrubURLUserInfo(uu.User)

	return &uu
}

func scrubURLUserInfo(userInfo *url.Userinfo) *url.Userinfo {
	if userInfo != nil {
		return url.UserPassword(userInfo.Username(), scrubbedValue)
	}

	return nil
}
