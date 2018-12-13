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
var privateKey = []byte(`-----BEGIN RSA PRIVATE KEY-----
ABCABCABCABCABCABCABCABCABCABCABCABCABCABCABCABCABCABCABCABCABC
-----END RSA PRIVATE KEY-----
`)
var hmacSecret = []byte(`ABCABCABC`)

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
