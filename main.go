package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/bradleyfalzon/ghinstallation"
	"github.com/google/uuid"
	"github.com/tink-ab/go-github/github"
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

const appID = 13053

const installationID = 199248

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

	if *event.Action != "closed" {
		return
	}

	transport := http.Transport{}
	// the installation id we get from event is 64 bit, not sure if we should use it as this intallation id
	itr, err := ghinstallation.New(&transport, appID, installationID, privateKey)
	if err != nil {
		log.Println("Unable to create a new transport:", err)
		return
	}
	client := github.NewClient(&http.Client{Transport: itr})

	if event.Number == nil {
		log.Println("Unexpected number, nil")
	}

	dependeepr := newPullRequestFrom(event)

	xRefs, err := listCrossReferences(r.Context(), client, dependeepr)

	if err != nil {
		log.Print("Unable to list cross references:", err)
		return
	}

	for _, xRef := range xRefs {
		if xRef.GetEvent() != "cross-referenced" {
			continue
		}

		log.Println("Found a cross-reference.")

		dependerpr, err := getDependerPrFromSource(r.Context(), client, *xRef.GetSource())
		if err != nil {
			log.Println(err)
		}
		// TODO: Avoid handling the same PR multiple times.
		if wasReferenced, err := hasPullRequestReference(r.Context(), client, dependeepr, dependerpr); err != nil {
			log.Println(err)
			// Not returning here to try to rebase the other dependencies.
		} else if !wasReferenced {
			log.Println("Wasn't referenced. Ignoring cross-reference.")
			continue
		}
		log.Println("Initiating rebase.")
		if err := handlePullRequestRebase(itr, client, dependerpr, dependeepr); err != nil {
			log.Print("can not rebase pull request ", err)
		}
	}
}

type pullRequest struct {
	Owner  string
	Repo   string
	Number int
	Base   string
}

func getDependerPrFromSource(ctx context.Context, client *github.Client, source github.Source) (pullRequest, error) {
	var dependerPr pullRequest
	issueID := source.Issue.GetNumber()
	owner := *source.Issue.Repository.Owner.Login
	repo := *source.Issue.Repository.Name
	prEvent, _, err := client.PullRequests.Get(ctx, owner, repo, issueID)

	if err != nil {
		return dependerPr, err
	}

	return pullRequest{
		Owner:  owner,
		Repo:   repo,
		Number: issueID,
		Base:   prEvent.Base.GetRef(),
	}, nil
}

func newPullRequestFrom(e github.PullRequestEvent) pullRequest {
	return pullRequest{
		Owner:  e.Repo.Owner.GetLogin(),
		Repo:   e.Repo.GetName(),
		Number: e.GetNumber(),
		Base:   e.GetPullRequest().GetBase().GetRef(),
	}
}

func listCrossReferences(ctx context.Context, client *github.Client, pr pullRequest) ([]*github.Timeline, error) {
	lo := &github.ListOptions{PerPage: 10}

	var crossReferenceTimelines []*github.Timeline
	for {
		timelines, resp, err := client.Issues.ListIssueTimeline(ctx, pr.Owner, pr.Repo, pr.Number, lo)
		if err != nil {
			log.Print("can not get xRef from event:", err)
			return nil, err
		}

		for _, timeline := range timelines {
			if *timeline.Event != "cross-referenced" {
				continue
			}
			crossReferenceTimelines = append(crossReferenceTimelines, timeline)
		}
		if resp.NextPage == 0 {
			break
		}
		lo.Page = resp.NextPage
	}

	return crossReferenceTimelines, nil
}

func hasPullRequestReference(ctx context.Context, client *github.Client, dependeepr, dependerpr pullRequest) (bool, error) {
	// TODO check if any comment matches the regexp "depends on #" + xRef.Number

	opt := &github.IssueListCommentsOptions{
		Sort:        "created",
		ListOptions: github.ListOptions{PerPage: 10},
	}

	needle := fmt.Sprintf("depends on #%d", dependeepr.Number)
	for {
		comments, resp, err := client.Issues.ListComments(ctx, dependerpr.Owner, dependerpr.Repo, dependerpr.Number, opt)
		if err != nil {
			return false, err
		}

		for _, comment := range comments {
			log.Println("Checking comment:", *comment.Body)
			hasReference := CaseInsensitiveContains(*comment.Body, needle)
			if hasReference {
				return true, nil
			}
		}

		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}

	return false, nil
}

func handlePullRequestRebase(itr *ghinstallation.Transport, client *github.Client, pr pullRequest, prDependee pullRequest) error {
	token, err := itr.Token()
	destBranch := pr.Base

	if err != nil {
		return err
	}

	dir, err := checkout(token, pr)
	if err != nil {
		return err
	}
	log.Println("Cloned the git repo. Location:", dir)
	defer os.RemoveAll(dir) // TODO: Log error.

	newBranch, err := rebase(dir, destBranch, pr)
	if err != nil {
		return err
	}

	if err := push(dir, newBranch); err != nil {
		return err
	}

	// do we have to return the pull request?
	newPrNumber, err := createPullRequest(dir, newBranch, client, pr, prDependee)
	if err != nil {
		return err
	}

	if err := closePullRequest(client, pr, newPrNumber); err != nil {
		return err
	}

	// TODO: Really bad name of this function :-) Let's fix that soon.
	if err := informTheUser(client, pr); err != nil {
		return err
	}

	return nil
}

func checkout(token string, pr pullRequest) (string, error) {
	dir, err := ioutil.TempDir("", "rebbot")
	if err != nil {
		return "", fmt.Errorf("cannot create temporary directory when checking out branch %s", err)
	}

	url := fmt.Sprintf("https://token:%s@github.com/%s/%s.git", token, pr.Owner, pr.Repo)
	err = exec.Command("git", "clone", url, dir).Run()
	if err != nil {
		return "", fmt.Errorf("git clone command failed %s", err)
	}
	return dir, nil
}

func rebase(gitRepo, destBranch string, pr pullRequest) (string, error) {
	tempBranch := uuid.New().String()
	err := execInPath(gitRepo, "git", "fetch", "origin", fmt.Sprintf("pull/%d/head:%s", pr.Number, tempBranch))
	if err != nil {
		return "", err
	}
	err = execInPath(gitRepo, "git", "checkout", tempBranch)
	if err != nil {
		return "", err
	}
	return tempBranch, execInPath(gitRepo, "git", "rebase", destBranch)
}

func execInPath(dir, cmd string, args ...string) error {
	log.Println("Executing:", cmd, args)
	c := exec.Command(cmd, args...)
	c.Dir = dir
	return c.Run()
}

func push(gitRepo, newBranch string) error {
	if err := execInPath(gitRepo, "git", "checkout", newBranch); err != nil {
		return err
	}
	return execInPath(gitRepo, "git", "push", "origin")
}

func createPullRequest(gitRepo, newBranch string, client *github.Client, pr pullRequest, prDependee pullRequest) (int, error) {
	title := fmt.Sprintf("A new PR rebased on #%d", pr.Number)
	body := fmt.Sprintf("Depends on #%d.", prDependee.Number)
	base := "master"

	data := &github.NewPullRequest{
		Title: &title,
		Body:  &body, // TODO: Correct?
		Base:  &base, // TODO: Handle a other destination branches
		Head:  &newBranch,
	}

	newPr, _, err := client.PullRequests.Create(context.TODO(), pr.Owner, pr.Repo, data)
	return *newPr.Number, err
}

func closePullRequest(client *github.Client, pr pullRequest, newPrNumber int) error {
	state := "closed"
	_, _, err := client.PullRequests.Edit(context.TODO(), pr.Owner, pr.Repo, pr.Number, &github.PullRequest{
		State: &state,
	})
	return err
}

func informTheUser(client *github.Client, pr pullRequest) error {
	body := fmt.Sprintf("Closed this as I created a [new pull request](#%d) which is rebased on the latest branch.", pr.Number)
	_, _, err := client.Issues.CreateComment(context.TODO(), pr.Owner, pr.Repo, pr.Number, &github.IssueComment{
		Body: &body,
	})
	return err
}

// Utility functions.

func CaseInsensitiveContains(s, substr string) bool {
	s, substr = strings.ToUpper(s), strings.ToUpper(substr)
	return strings.Contains(s, substr)
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
