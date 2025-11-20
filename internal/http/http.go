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

func GetLang(r *http.Request, defaultLanguage string, languages []language.Tag) language.Tag {
	log := logf.FromContext(r.Context())

	l10n := GetParam("l10n", "", r)

	if l10n != "" {
		tag := language.Make(l10n)
		if tag.IsRoot() {
			log.Info("parsing user supplied 'l10n' parameter failed, falling back to default", "l10n", l10n, "defaultLang", defaultLanguage, "path", r.URL.Path)
			return language.Make(defaultLanguage)
		} else {
			return tag
		}
	}

	preferredLanguages := []language.Tag{}

	acceptLanguages := strings.Split(r.Header.Get("Accept-Language"), ",")

	for _, acceptLanguage := range acceptLanguages {
		acceptLanguage = strings.TrimSpace(acceptLanguage)
		if acceptLanguage == "" {
			continue
		}
		prefix := strings.SplitN(acceptLanguage, ";", 2)[0]
		tag := language.Make(prefix)
		preferredLanguages = append(preferredLanguages, tag)
	}

	matcher := language.NewMatcher(languages)

	_, index, _ := matcher.Match(preferredLanguages...)

	matchedTag := languages[index]

	if matchedTag.IsRoot() {
		return language.Make(defaultLanguage)
	}

	return matchedTag
}
