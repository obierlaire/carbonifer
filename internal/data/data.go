package data

import (
	"embed"
	"io/fs"
	"io/ioutil"
	"os"
	"path/filepath"

	log "github.com/sirupsen/logrus"

	"github.com/spf13/viper"
)

//go:embed data/*
var data embed.FS

func ReadDataFile(filename string) []byte {
	dataPath := viper.GetString("data.path")
	if dataPath != "" {
		// If the environment variable is set, read from the specified file
		filePath := filepath.Join(dataPath, filename)
		if _, err := os.Stat(filePath); !os.IsNotExist(err) {
			log.Debugf("  reading datafile '%v' from: %v", filename, filePath)
			data, err := ioutil.ReadFile(filePath)
			if err != nil {
				log.Fatal(err)
			}
			return data
		} else {
			return readEmbeddedFile(filename)
		}
	} else {
		// Otherwise, read from the embedded file
		return readEmbeddedFile(filename)
	}
}

func readEmbeddedFile(filename string) []byte {
	log.Debugf("  reading datafile '%v' embedded", filename)
	data, err := fs.ReadFile(data, "data/"+filename)
	if err != nil {
		log.Fatal(err)
	}
	return data
}
