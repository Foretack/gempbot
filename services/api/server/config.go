package server

import (
	"encoding/json"
	"io/ioutil"
	"net/http"

	"github.com/go-redis/redis/v7"
	log "github.com/sirupsen/logrus"
)

type UserConfig struct {
	Redemptions Redemptions
	Editors     []string
	Protected   Protected
}

type Protected struct {
	EditorFor []string
}

func createDefaultUserConfig() UserConfig {
	return UserConfig{
		Redemptions: Redemptions{
			Bttv: Redemption{Title: "Bttv emote", Active: false},
		},
		Editors: []string{},
		Protected: Protected{
			EditorFor: []string{},
		},
	}
}

func (s *Server) handleUserConfig(w http.ResponseWriter, r *http.Request) {
	ok, auth, _ := s.authenticate(r)
	if !ok {
		http.Error(w, "bad authentication", http.StatusUnauthorized)
		return
	}

	if r.Method == http.MethodGet {
		val, err := s.store.Client.HGet("userConfig", auth.Data.UserID).Result()
		if err != nil || val == "" {
			writeJSON(w, createDefaultUserConfig(), http.StatusOK)
			return
		}

		var userConfig UserConfig
		if err := json.Unmarshal([]byte(val), &userConfig); err != nil {
			log.Errorf("can't unmarshal saved config %s", err)
			http.Error(w, "can't recover config"+err.Error(), http.StatusBadRequest)
			return
		}

		writeJSON(w, userConfig, http.StatusOK)
	} else if r.Method == http.MethodPost {
		body, err := ioutil.ReadAll(r.Body)
		if err != nil {
			log.Errorf("Failed reading update body: %s", err)
			http.Error(w, "failed reading body"+err.Error(), http.StatusInternalServerError)
			return
		}

		err = s.processConfig(auth.Data.UserID, body)
		if err != nil {
			log.Errorf("failed processing config: %s", err)
			http.Error(w, "failed processing config: "+err.Error(), http.StatusBadRequest)
			return
		}

		writeJSON(w, "", http.StatusOK)
	} else if r.Method == http.MethodDelete {
		_, err := s.store.Client.HDel("userConfig", auth.Data.UserID).Result()
		if err != nil {
			log.Error(err)
			http.Error(w, "Failed deleting: "+err.Error(), http.StatusInternalServerError)
			return
		}

		err = s.unsubscribeChannelPoints(auth.Data.UserID)
		if err != nil {
			log.Error(err)
			http.Error(w, "Failed to unsubscribe"+err.Error(), http.StatusInternalServerError)
			return
		}

		writeJSON(w, "", http.StatusOK)
	}

}

func (s *Server) processConfig(userID string, body []byte) error {
	isNew := false

	val, err := s.store.Client.HGet("userConfig", userID).Result()
	if err == redis.Nil {
		isNew = true
	} else if err != nil {
		return err
	}

	var oldConfig UserConfig
	if !isNew {
		if err := json.Unmarshal([]byte(val), &oldConfig); err != nil {
			return err
		}
	}

	var newConfig UserConfig
	if err := json.Unmarshal(body, &newConfig); err != nil {
		return err
	}

	protected := oldConfig.Protected
	if protected.EditorFor != nil {
		protected.EditorFor = []string{}
	}

	configToSave := UserConfig{
		Redemptions: newConfig.Redemptions,
		Editors:     newConfig.Editors,
		Protected:   protected,
	}

	js, err := json.Marshal(configToSave)
	if err != nil {
		return err
	}

	_, err = s.store.Client.HSet("userConfig", userID, js).Result()
	if err != nil {
		return err
	}

	if isNew {
		log.Info("Created new config for: ", userID)
		s.subscribeChannelPoints(userID)
	}

	return nil
}

func writeJSON(w http.ResponseWriter, data interface{}, code int) {
	js, err := json.Marshal(data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_, err = w.Write(js)
	if err != nil {
		log.Errorf("Faile to writeJSON: %s", err)
	}
}
