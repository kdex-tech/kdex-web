package http

import (
	"net/http"
	"strings"

	"golang.org/x/text/language"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

func GetParam(name string, defaultValue string, r *http.Request) string {
	value := r.PathValue(name)

	if value == "" {
		value = r.URL.Query().Get(name)
	}

	if value == "" {
		return defaultValue
	} else {
		return value
	}
}

func GetParamArray(name string, defaultValue []string, r *http.Request) []string {
	value := r.PathValue(name)
	var values []string

	if value == "" {
		values = r.URL.Query()[name]
	} else {
		values = []string{value}
	}

	if len(values) == 0 {
		return defaultValue
	} else {
		return values
	}
}

func GetLang(r *http.Request, defaultLang string, supportedLangs []language.Tag) language.Tag {
	log := logf.FromContext(r.Context())

	fromParams := GetParam("l10n", "", r)

	if fromParams != "" {
		tag := language.Make(fromParams)
		if tag.IsRoot() {
			log.Info("parsing user supplied 'l10n' parameter failed, falling back to default", "l10n", fromParams, "defaultLang", defaultLang)
			return language.Make(defaultLang)
		} else {
			return tag
		}
	}

	preferences := []language.Tag{}

	headerLangs := strings.Split(r.Header.Get("Accept-Language"), ",")

	for _, headerLang := range headerLangs {
		headerLang = strings.TrimLeft(headerLang, ";")
		preferences = append(preferences, language.Make(headerLang))
	}

	matcher := language.NewMatcher(supportedLangs)

	tag, _, _ := matcher.Match(preferences...)

	if tag.IsRoot() {
		return language.Make(defaultLang)
	}

	return tag
}
