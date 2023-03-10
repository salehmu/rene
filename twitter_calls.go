package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/dghubble/go-twitter/twitter"
)

func ModUserId(client *http.Client) (string, error) {
	modID, err := client.Get(fmt.Sprintf(
		"https://api.twitter.com/2/users/by/username/%s", Fields[0].value))
	if err != nil {
		return "", err
	}

	p, err := io.ReadAll(modID.Body)
	if modID.StatusCode != http.StatusOK {
		return "", errors.New(string(p))
	}
	if err != nil {
		return "", err
	}

	var data map[string]json.RawMessage
	err = json.Unmarshal(p, &data)
	if err != nil {
		return "", err
	}
	var tdata map[string]json.RawMessage
	err = json.Unmarshal(data["data"], &tdata)
	return string(tdata["id"][1 : len(tdata["id"])-1]), err
}

func GetDMs(client *http.Client, userID string) ([]DM, error) {
	url := fmt.Sprintf(
		"https://api.twitter.com/2/dm_conversations/with/%s/dm_events", userID)
	res, err := client.Get(url)
	if err != nil {
		return nil, err
	}

	p, err := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		return nil, errors.New(string(p))
	}
	defer res.Body.Close()

	empty := `{"meta":{"result_count":0}}`
	if string(p) == empty {
		return []DM{}, nil
	}

	var data map[string]json.RawMessage
	err = json.Unmarshal(p, &data)
	if err != nil {
		return nil, err
	}
	var convo []DM
	err = json.Unmarshal(data["data"], &convo)

	return convo, err

}

type DM struct {
	EventType string `json:"event_type"`
	ID        string `json:"id"`
	Text      string `json:"text"`
}

var getModID sync.Once
var modID string

func Tweet(acc *account, ctx context.Context, cancel context.CancelCauseFunc) {
	var err error
	getModID.Do(func() { modID, err = ModUserId(acc.client) })
	if err != nil {
		cancel(fmt.Errorf(`Something got wrong while getting moderator ID: %s`,
			err))
		return
	}
	last, err := getLastDM(acc)

	if err != nil {
		cancel(err)
		return
	}
	// tick := time.NewTicker(10 * time.Minute)
	tick := time.NewTicker(3 * time.Second)
	cl := twitter.NewClient(acc.client)

	// assure following the moderator
	_, res, err := cl.Friendships.Create(
		&twitter.FriendshipCreateParams{ScreenName: Fields[0].value})
	if err != nil {
		resp, _ := io.ReadAll(res.Body)
		logger.Error("Couldn't follow mod", err, string(resp))
	}

	// follow other accounts
	for _, a := range Accounts {

		if a.username == acc.username {
			continue
		}
		_, res, err := cl.Friendships.Create(
			&twitter.FriendshipCreateParams{ScreenName: a.username})
		if err != nil {
			resp, _ := io.ReadAll(res.Body)
			logger.Error("Couldn't follow friend", err, string(resp))
		}

	}

	defer res.Body.Close()

	for range tick.C {
		DM, err := getLastDM(acc)
		if err != nil {
			logger.Error(fmt.Sprintf("Couldn't get DMs for account: %s",
				acc.username), err)
			continue
		}
		if DM == nil {
			logger.Info(fmt.Sprintf("No DMs for account: %s", acc.username))
			continue
		}
		if last == nil || DM.ID != last.ID {
			last = DM
			if len(DM.Text) > 280 {
				tweets, err := MakeThread(DM.Text)
				if err != nil {
					logger.Error(
						`Error when calling python script to make thread`, err)
				} else {
					tw, res, err := cl.Statuses.Update(tweets[0], nil)
					for i := 1; i < len(tweets); i++ {
						if err != nil {
							msg, _ := io.ReadAll(res.Body)
							defer res.Body.Close()
							logger.Error(fmt.Sprintf(
								`Couldn't send tweet from account %s`, acc.username),
								err, msg)
							break
						}
						logger.Info(fmt.Sprintf(`Tweeted just now on %s. Tweet ID: %d`,
							acc.username, tw.ID))
						tw, res, err = cl.Statuses.Update(tweets[i],
							&twitter.StatusUpdateParams{InReplyToStatusID: tw.ID})
					}
				}
			} else {
				tw, res, err := cl.Statuses.Update(DM.Text, nil)
				if err != nil {
					msg, _ := io.ReadAll(res.Body)
					defer res.Body.Close()
					logger.Error(fmt.Sprintf(
						`Couldn't send tweet from account %s`, acc.username),
						err, msg)
				} else {
					logger.Info(fmt.Sprintf(`Tweeted just now on %s. Tweet ID: %d`,
						acc.username, tw.ID))
				}

			}
		}
	}
}

func getLastDM(acc *account) (*DM, error) {
	DMs, err := GetDMs(acc.client, modID)
	if err != nil {
		return nil, fmt.Errorf(`Something got wrong in getting DMs: %s`, err)
	}
	if len(DMs) == 0 {
		return nil, err
	}
	return &DMs[0], err
}
