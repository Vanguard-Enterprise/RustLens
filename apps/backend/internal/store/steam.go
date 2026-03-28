package store

import (
	"errors"
	"time"
)

var ErrSteamAccountNotFound = errors.New("steam account not found")
var ErrLinkAlreadyExists = errors.New("steam discord link already exists")

type SteamAccount struct {
	SteamID   string
	BmID      *string
	CreatedAt time.Time
}

type SteamDiscordLink struct {
	SteamID   string
	DiscordID string
	Type      string
	Source    string
	CreatedAt time.Time
}

func scanSteamAccount(scanner rowScanner) (SteamAccount, error) {
	var record SteamAccount
	err := scanner.Scan(&record.SteamID, &record.BmID, &record.CreatedAt)
	if err != nil {
		return SteamAccount{}, err
	}
	return record, nil
}

func scanSteamDiscordLink(scanner rowScanner) (SteamDiscordLink, error) {
	var record SteamDiscordLink
	err := scanner.Scan(&record.SteamID, &record.DiscordID, &record.Type, &record.Source, &record.CreatedAt)
	if err != nil {
		return SteamDiscordLink{}, err
	}
	return record, nil
}
