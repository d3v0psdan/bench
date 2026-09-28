//go:build !linux

package services

func linkLibaio(string) error { return nil } // a Linux (Ubuntu 24.04) quirk only

func missingMySQLLibs() []string { return nil } // Linux-only system dependencies
