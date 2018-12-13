package main

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/bradleyfalzon/ghinstallation"
	"github.com/google/go-github/github"
)

var commitStatusContext = "tink/four-eyes"

// The following two fields must be set when creating a new app.
// Versioned secrets is for "Test Rebbot" Github app, which only
// is connected to https://github.com/rebbot/test-repo to be used
// for testing.
var privateKey = []byte(`-----BEGIN RSA PRIVATE KEY-----
MIIEowIBAAKCAQEA0BPeSw4SG1brGRB2ejxL/pRtTguiIQsjE+np8zVodcO1tU0j
peLgbuaPaEyO9Yu2eSaynCRlq1afGzvwPHMHDxXPu29ibd5jAo1aZaeWTC2yogVZ
IO7ndZodDHQWksQLSgc3GV3TV4n9zaj4s7+XASQa85gVxTBqb1ZadX8/muW2kMmU
Ay7n99RMAi3lseNZAWbCdWjiDEVp3KlRvb9tLDywYct+ybjf9N25wEUUfgeY9wI4
DmKseX9r4cJHiYEE+bd+96SjZiMzctzEjavj4JIX3i6wDFg3RTpSi6Hqs3QwtxMt
eWuwYLf67mFMbolGdhD7dvxiS3f21MObqnF9gwIDAQABAoIBAFCtA2lSYU7cWnbz
XRxzuHtSjTbPZ+Mr8EPOU/kKYiAW66MJ76Jn3uDg4AVueZdWvj8m8+V6bzkJctMa
YEDv0HLW4B4qR52VtgnNSJlVav7KURJkxHPybSe5wz2K3R22iTIAripVqJWuWQue
Uh9PT3sPxqtf8kDsTrgwYQ5hcgXaC1wzFt0SocWPVLY3hdu338Y4m9cUfap3BjxB
Fl6KOZLMtbnaJOqLlQjM84vu2QXWzTtsUqmyuBYZOgpGz+17AeT8ekU73rXOWPoe
QHZl53JNV1hV3xUOHdG821XOXp2Jdb5ECHUov5lS7mF99CZ+HzEGtY8ubgjgBVUD
xn/zqNECgYEA+ycxNkPyKD2R83F/ga1ksKBWNF8N8CnNq8CmrhemtEj112Y3mMaA
yoyTVOWb+2alRFpGw82WVWoHtfkJgNM+3tO9HpnA2029UPlsVATMI0OQ3Egd0bXs
o7J/gjDZTtZyaYTrjkj5vktZ731NG+m9hLFMEzo69tD+fM/Q2en/S20CgYEA1Bfd
NNypJR5AEPp2xvWgiMN4oHeDVlIMijtKCv/lqYDdDqKjkS0QjfAovZ/HkxwudG0V
QoDwwKLEiha8GU1PZMDIg4fW95ImqurZQG5fqc1Lm7kS1DFp4AWiw8EldCZFGuE3
XxoG0rCS8LLxwe77Nlej5XUxzUBDDPWkw0kt5q8CgYAiIEQulHLuBtezFYP20eGx
oke0XAofzP5WTRoY47vSGWvWNdxuFOLhItLOIVjdgygHrqCY8HFx77NWhZ1F9O5B
BtJWuxuacOi9fPa8P96hGAgx9lae7TJXV+S9gve0H61yKw56ye2tbr2srgDxPwRy
aEjm/+2NJf6+ZNqDEamPzQKBgQCkcwh6l2mzNRxZzcpRBF0ADgg269PzF1VPzR7h
Hn91iUxdr6+Bvl5qn78HIJ9/Kke+0GG+mfmSc+JOa8hXGgGoTm5qxeXhOfovZj8j
XTFhmKO6T6sQymucXuJQRC+FOrM0X1IutCB8NpsIdMdNJr6z6QpUvSTrT5ttrf2d
yd0EUwKBgEdqsLuu74PwBR/pSk1ts76qhThZv9H2yJ/Ls9zUcPlUOifLZJDCgoaG
pgoDbPyrul8+jqX/NOG4p33N1Btj1ZDGA8C6CcvhCI0zWydJRA4u6Dg6rSyjvv83
XveJWmPnDanYTPGhJNegvmg1rNj82zFlJmbJTK9sXQJ5ZO8GbpOS
-----END RSA PRIVATE KEY-----
`)
var hmacSecret = []byte(`C8A40DA0-1E83-43E3-B890-85261B386DAF`)

const integrationID = 22228

const installationID = 512935

func main() {
	http.HandleFunc("/webhook", webhookHandler)
	http.HandleFunc("/_ah/health", healtCheckHandler)
	log.Print("Listening on port 8080")
	http.ListenAndServe(":8080", nil)
}

func healtCheckHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprint(w, "ok")
}

func webhookHandler(w http.ResponseWriter, r *http.Request) {
	event := r.Header.Get("X-GitHub-Event")
	switch event {
	case "ping":
		handlePing(w, r)
	case "pull_request":
		// https://developer.github.com/v3/activity/events/types/#pullrequestevent
		handlePullRequestEvent(w, r)
	case "issue_comment":
		// https://developer.github.com/v3/activity/events/types/#issuecommentevent
		handleIssueCommentEvent(w, r)
	default:
		log.Println("unrecognized event:", event)
		w.WriteHeader(204)
	}
}

func handlePing(w http.ResponseWriter, r *http.Request) {
	if err := decodeJSONOrBail(w, r, &struct{}{}); err != nil {
		return
	}
	w.WriteHeader(204)
}

func handlePullRequestEvent(w http.ResponseWriter, r *http.Request) {
	var event github.PullRequestEvent

	if err := decodeJSONOrBail(w, r, &event); err != nil {
		return
	}

	switch *event.Action {
	case "closed":
		handleClosedPullRequest(event)
	case "opened":
		handleOpenedPullRequest(event)
	}
}

func handleClosedPullRequest(event github.PullRequestEvent) {
	// TODO: Implement.
}

func handleOpenedPullRequest(event github.PullRequestEvent) {
	transport := http.Transport{}
	itr, err := ghinstallation.New(&transport, integrationID, installationID, privateKey)
	if err != nil {
		log.Println("Unable to create a new transport:", err)
		return
	}

	prno, err := parsePullRequestNumber(*event.GetPullRequest().Body)
	if err != nil {
		log.Println("Unable to parse pull request number:", err)
		return
	}

	if event.Number == nil {
		log.Println("Expected pull request number. Was nil.")
		return
	}

	err = savePullRequestDep(prno, event.GetNumber())
	if err != nil {
		log.Println("Unable to save pull request dependency.", err)
		return
	}

	client := github.NewClient(&http.Client{Transport: itr})

}

func handleIssueCommentEvent(w http.ResponseWriter, r *http.Request) {
	var event github.IssueCommentEvent

	if err := decodeJSONOrBail(w, r, &event); err != nil {
		return
	}

	if a := *event.Action; a != "created" {
		log.Println("Issue comment isn't created. Ignoring. Action:", a)
		return
	}

	if *event.Issue.State == "closed" {
		log.Println("Not reacting to closed issue.")
		return
	}

	prno, err := parsePullRequestNumber(*event.Comment.Body)
	if err != nil {
		log.Println("Unable to parse issue number :", err)
		return
	}

	err = savePullRequestDep(prno, event.GetIssue().GetNumber())
	if err != nil {
		log.Println("Unable to save issue dependency.", err)
		return
	}
}

// Utility functions.

func parsePullRequestNumber(s)

func isRelevantRef(ref string) bool {
	return ref == "refs/heads/staging" || ref == "refs/heads/trying"
}

func decodeJSONOrBail(w http.ResponseWriter, r *http.Request, m interface{}) error {
	err := decodeAndValidateJSON(r, &m)
	if err != nil {
		log.Println(err)
		if err == errIncorrectSignature {
			w.WriteHeader(401)
			return err
		}
		w.WriteHeader(400)
	}
	return err
}

var errIncorrectSignature = errors.New("signature is incorrect")

func decodeAndValidateJSON(r *http.Request, m interface{}) error {
	givenHmacString := r.Header.Get("X-Hub-Signature")

	if givenHmacString == "" {
		return errIncorrectSignature
	}

	pieces := strings.SplitN(givenHmacString, "=", 2)
	if len(pieces) < 2 {
		return errors.New("malformed signature")
	}
	if pieces[0] != "sha1" {
		return errors.New("hmac type not supported: " + pieces[0])
	}

	givenHmac, err := hex.DecodeString(pieces[1])
	if err != nil {
		return err
	}

	hmacer := hmac.New(sha1.New, hmacSecret)
	teeReader := io.TeeReader(r.Body, hmacer)

	if err := json.NewDecoder(teeReader).Decode(m); err != nil {
		return err
	}

	expectedMAC := hmacer.Sum(nil)
	if !hmac.Equal(givenHmac, expectedMAC) {
		return errIncorrectSignature
	}

	return nil
}
