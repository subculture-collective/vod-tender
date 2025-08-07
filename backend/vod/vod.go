package vod

import (
	"database/sql"
	"fmt"
	"time"
)

func StartVODProcessingJob(db *sql.DB) {
	for {
		ProcessUnprocessedVODs(db)
		time.Sleep(1 * time.Hour)
	}
}

func ProcessUnprocessedVODs(db *sql.DB) {
	fmt.Println("Processing unprocessed VODs (not yet implemented)")
}
