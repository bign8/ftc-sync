package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
)

// trying to re-use client between calls

var _ http.CookieJar = (*fsJar)(nil)

// func init() {
// 	jar, err := cookiejar.New(nil)
// 	if err != nil {
// 		panic(err)
// 	}
// 	client.Jar = jar
// }

func newFsJar(file string) http.CookieJar {
	fh, err := os.OpenFile(".cookies", os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		panic(err)
	}
	return &fsJar{
		fh: fh,
	}
}

type fsJar struct {
	fh *os.File
}

func (jar *fsJar) Cookies(u *url.URL) []*http.Cookie {
	_, err := jar.fh.Seek(0, 0) // seek to start
	if err != nil {
		panic(err)
	}

	var cookies []string
	err = json.NewDecoder(jar.fh).Decode(&cookies)
	switch err {
	case nil:
		// noop
	case io.EOF:
		// noop
	default:
		panic(err)
	}

	result := make([]*http.Cookie, 0, len(cookies))
	for _, line := range cookies {
		cookie, err := http.ParseSetCookie(line)
		if err != nil {
			panic(err)
		}
		result = append(result, cookie)
	}

	slog.Debug("getting cookies for", slog.String("url", u.String()), slog.Any("data", result))
	return result
}

func (jar *fsJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	_, err := jar.fh.Seek(0, 0) // seek to start
	if err != nil {
		panic(err)
	}

	lines := make([]string, 0, len(cookies))
	for _, cookie := range cookies {
		lines = append(lines, cookie.Raw)
	}

	enc := json.NewEncoder(jar.fh)
	enc.SetIndent("", "\t")
	enc.SetEscapeHTML(false)

	if err := enc.Encode(lines); err != nil {
		panic(err)
	}

	slog.Debug("setting cookies for", slog.String("url", u.String()))
}
