// Copyright 2023 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package samples

import (
	"cloud.google.com/go/spanner"
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"strings"
	"time"

	"cloud.google.com/go/civil"
	spannergorm "github.com/googleapis/go-gorm-spanner"
	_ "github.com/googleapis/go-sql-spanner"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

//go:embed create_data_model.sql
var createDataModelSQL string

type Singer struct {
	gorm.Model
	FirstName sql.NullString
	LastName  string
	// FullName is generated by the database. The '->' marks this a read-only field.
	FullName string `gorm:"->;type:STRING(MAX) AS (ARRAY_TO_STRING([first_name, last_name], \" \")) STORED;"`
	Active   bool
	Albums   []Album
}

type Album struct {
	gorm.Model
	Title           string
	MarketingBudget sql.NullFloat64
	ReleaseDate     spanner.NullDate
	CoverPicture    []byte
	SingerId        int64
	Singer          Singer
	Tracks          []Track `gorm:"foreignKey:id"`
}

// Track is interleaved in Album. The ID column is both the first part of the primary key of Track, and a
// reference to the Album that owns the Track.
type Track struct {
	gorm.Model
	TrackNumber int64 `gorm:"primaryKey;autoIncrement:false"`
	Title       string
	SampleRate  float64
	Album       Album `gorm:"foreignKey:id"`
}

type Venue struct {
	gorm.Model
	Name        string
	Description string
}

type Concert struct {
	gorm.Model
	Name      string
	Venue     Venue
	VenueId   int64
	Singer    Singer
	SingerId  int64
	StartTime time.Time
	EndTime   time.Time
}

var rnd = rand.New(rand.NewSource(time.Now().UnixNano()))

func RunSample(w io.Writer, connString string) error {
	db, err := gorm.Open(spannergorm.New(spannergorm.Config{
		DriverName: "spanner",
		DSN:        connString,
	}), &gorm.Config{PrepareStmt: true, IgnoreRelationshipsWhenMigrating: true}) //Logger: logger.Default.LogMode(logger.Error),

	if err != nil {
		return fmt.Errorf("failed to open gorm connection: %w", err)
	}
	//Create the sample tables if they do not yet exist.
	if err := CreateInterleavedTablesIfNotExist(w, db); err != nil {
		return err
	}
	if err := db.AutoMigrate(&Venue{}, &Concert{}); err != nil {
		return fmt.Errorf("failed to migrate: %w", err)
	}

	fmt.Fprintf(w, "Starting sample...")

	// Delete all existing data to start with a clean database.
	if err := DeleteAllData(db); err != nil {
		return fmt.Errorf("failed to delete all data: %w", err)
	}
	fmt.Fprintf(w, "Purged all existing test data\n\n")

	// Create some random Singers, Albums and Tracks.
	if err := CreateRandomSingersAndAlbums(w, db); err != nil {
		return err
	}
	// Print the generated Singers, Albums and Tracks.
	if err := PrintSingersAlbumsAndTracks(w, db); err != nil {
		return err
	}

	// Create a Concert for a random singer.
	if err := CreateVenueAndConcertInTransaction(w, db); err != nil {
		return err
	}
	// Print all Concerts in the database.
	if err := PrintConcerts(w, db); err != nil {
		return err
	}
	// Print all Albums that were released before 1900.
	if err := PrintAlbumsReleaseBefore1900(w, db); err != nil {
		return err
	}
	// Print all Singers ordered by last name.
	// The function executes multiple queries to fetch a batch of singers per query.
	if err := PrintSingersWithLimitAndOffset(w, db); err != nil {
		return err
	}
	// Print all Albums that have a title where the first character of the title matches
	// either the first character of the first name or first character of the last name
	// of the Singer.
	if err := PrintAlbumsFirstCharTitleAndFirstOrLastNameEqual(w, db); err != nil {
		return err
	}
	// Print all Albums whose title start with 'e'. The function uses a named argument for the query.
	if err := SearchAlbumsUsingNamedArgument(w, db, "e%"); err != nil {
		return err
	}

	// Update Venue description.
	if err := UpdateVenueDescription(w, db); err != nil {
		return err
	}
	// Use FirstOrInit to create or update a Venue.
	if err := FirstOrInitVenue(w, db, "Berlin Arena"); err != nil {
		return err
	}
	// Use FirstOrCreate to create a Venue if it does not already exist.
	if err := FirstOrCreateVenue(w, db, "Paris Central"); err != nil {
		return err
	}
	// Update all Tracks by fetching them in batches and then applying an update to each record.
	if err := UpdateTracksInBatches(w, db); err != nil {
		return err
	}

	// Delete a random Track from the database.
	if err := DeleteRandomTrack(w, db); err != nil {
		return err
	}
	// Delete a random Album from the database. This will also delete any child Track records interleaved with the
	// Album.
	if err := DeleteRandomAlbum(w, db); err != nil {
		return err
	}

	// Try to execute a query with a 1ms timeout. This will normally fail.
	if err := QueryWithTimeout(w, db); err != nil {
		return err
	}

	fmt.Fprintf(w, "Finished running sample\n")
	return nil
}

// CreateRandomSingersAndAlbums creates some random test records and stores these in the database.
func CreateRandomSingersAndAlbums(w io.Writer, db *gorm.DB) error {
	fmt.Fprintf(w, "Creating random singers and albums")
	if err := db.Transaction(func(tx *gorm.DB) error {
		// Create between 5 and 10 random singers.
		for i := 0; i < randInt(5, 10); i++ {
			singerId, err := CreateSinger(db, randFirstName(), randLastName())
			if err != nil {
				return fmt.Errorf("failed to create singer: %w", err)
			}
			fmt.Fprintf(w, ".")
			// Create between 2 and 12 random albums
			for j := 0; j < randInt(2, 12); j++ {
				_, err = CreateAlbumWithRandomTracks(db, randAlbumTitle(), singerId, randInt(1, 22))
				if err != nil {
					return fmt.Errorf("failed to create album: %w", err)
				}
				fmt.Fprintf(w, ".")
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("failed to create random singers and albums: %w", err)
	}
	fmt.Fprintf(w, "Created random singers and albums\n\n")
	return nil
}

// PrintSingersAlbumsAndTracks queries and prints all Singers, Albums and Tracks in the database.
func PrintSingersAlbumsAndTracks(w io.Writer, db *gorm.DB) error {
	fmt.Fprintf(w, "Fetching all singers, albums and tracks")
	var singers []*Singer
	// Preload all associations of Singer.
	if err := db.Model(&Singer{}).Preload(clause.Associations).Order("last_name").Find(&singers).Error; err != nil {
		return fmt.Errorf("failed to load all singers: %w", err)
	}
	for _, singer := range singers {
		fmt.Fprintf(w, "Singer: {%v %v}\n", singer.ID, singer.FullName)
		fmt.Fprintf(w, "Albums:\n")
		for _, album := range singer.Albums {
			fmt.Fprintf(w, "\tAlbum: {%v %v}\n", album.ID, album.Title)
			fmt.Fprintf(w, "\tTracks:\n")
			if err := db.Model(&album).Preload(clause.Associations).Find(&album).Error; err != nil {
				return fmt.Errorf("failed to load album: %w", err)
			}
			for _, track := range album.Tracks {
				fmt.Fprintf(w, "\t\tTrack: {%v %v}\n", track.TrackNumber, track.Title)
			}
		}
	}
	fmt.Fprintf(w, "Fetched all singers, albums and tracks\n\n")
	return nil
}

// CreateVenueAndConcertInTransaction creates a new Venue and a Concert in a read/write transaction.
func CreateVenueAndConcertInTransaction(w io.Writer, db *gorm.DB) error {
	if err := db.Transaction(func(tx *gorm.DB) error {
		// Load the first singer from the database.
		singer := Singer{}
		if res := tx.First(&singer); res.Error != nil {
			return fmt.Errorf("failed to load singer: %w", res.Error)
		}
		// Create and save a Venue and a Concert for this singer.
		venue := Venue{
			Name:        "Avenue Park",
			Description: `{"Capacity": 5000, "Location": "New York", "Country": "US"}`,
		}
		if res := tx.Create(&venue); res.Error != nil {
			return fmt.Errorf("failed to create venue: %w", res.Error)
		}
		concert := Concert{
			Name:      "Avenue Park Open",
			VenueId:   int64(venue.ID),
			SingerId:  int64(singer.ID),
			StartTime: parseTimestamp("2023-02-01T20:00:00-05:00"),
			EndTime:   parseTimestamp("2023-02-02T02:00:00-05:00"),
		}
		if res := tx.Create(&concert); res.Error != nil {
			return fmt.Errorf("failed to create concert: %w", res.Error)
		}
		// Return nil to instruct `gorm` to commit the transaction.
		return nil
	}); err != nil {
		return fmt.Errorf("failed to create a Venue and a Concert: %w", err)
	}
	fmt.Fprintf(w, "Created a Venue and a Concert\n\n")
	return nil
}

// PrintConcerts prints the current concerts in the database to the console.
// It will preload all its associations, so it can directly print the properties of these as well.
func PrintConcerts(w io.Writer, db *gorm.DB) error {
	var concerts []*Concert
	if err := db.Model(&Concert{}).Preload(clause.Associations).Find(&concerts).Error; err != nil {
		return fmt.Errorf("failed to load concerts: %w", err)
	}
	for _, concert := range concerts {
		fmt.Fprintf(w, "Concert %q starting at %v will be performed by %s at %s\n",
			concert.Name, concert.StartTime, concert.Singer.FullName, concert.Venue.Name)
	}
	fmt.Fprintf(w, "Fetched all concerts\n\n")
	return nil
}

// UpdateVenueDescription updates the description of the 'Avenue Park' Venue.
func UpdateVenueDescription(w io.Writer, db *gorm.DB) error {
	if err := db.Transaction(func(tx *gorm.DB) error {
		venue := Venue{}
		if res := tx.Find(&venue, "name = ?", "Avenue Park"); res != nil {
			return res.Error
		}
		// Update the description of the Venue.
		venue.Description = `{"Capacity": 10000, "Location": "New York", "Country": "US", "Type": "Park"}`

		if res := tx.Update("description", &venue); res.Error != nil {
			return res.Error
		}
		// Return nil to instruct `gorm` to commit the transaction.
		return nil
	}); err != nil {
		return fmt.Errorf("failed to update Venue 'Avenue Park': %w", err)
	}
	fmt.Fprintf(w, "Updated Venue 'Avenue Park'\n\n")
	return nil
}

// FirstOrInitVenue tries to fetch an existing Venue from the database based on the name of the venue, and if not found,
// initializes a Venue struct. This can then be used to create or update the record.
func FirstOrInitVenue(w io.Writer, db *gorm.DB, name string) error {
	venue := Venue{}
	if err := db.Transaction(func(tx *gorm.DB) error {
		// Use FirstOrInit to search and otherwise initialize a Venue entity.
		// Note that we do not assign an ID in case the Venue was not found.
		// This makes it possible for us to determine whether we need to call Create or Save, as Cloud Spanner does not
		// support `ON CONFLICT UPDATE` clauses.
		if err := tx.FirstOrInit(&venue, Venue{Name: name}).Error; err != nil {
			return err
		}
		venue.Description = `{"Capacity": 2000, "Location": "Europe/Berlin", "Country": "DE", "Type": "Arena"}`
		// Create or update the Venue.
		if venue.ID == 0 {
			return tx.Create(&venue).Error
		}
		return tx.Update("description", &venue).Error
	}); err != nil {
		return fmt.Errorf("failed to create or update Venue %q: %w", name, err)
	}
	fmt.Fprintf(w, "Created or updated Venue %q\n\n", name)
	return nil
}

// FirstOrCreateVenue tries to fetch an existing Venue from the database based on the name of the venue, and if not
// found, creates a new Venue record in the database.
func FirstOrCreateVenue(w io.Writer, db *gorm.DB, name string) error {
	venue := Venue{}
	if err := db.Transaction(func(tx *gorm.DB) error {
		// Use FirstOrCreate to search and otherwise create a Venue record.
		// Note that we manually assign the ID using the Attrs function. This ensures that the ID is only assigned if
		// the record is not found.
		return tx.Where(Venue{Name: name}).Attrs(Venue{
			Description: `{"Capacity": 5000, "Location": "Europe/Paris", "Country": "FR", "Type": "Stadium"}`,
		}).FirstOrCreate(&venue).Error
	}); err != nil {
		return fmt.Errorf("failed to create Venue %q if it did not exist: %w", name, err)
	}
	fmt.Fprintf(w, "Created Venue %q if it did not exist\n\n", name)
	return nil
}

// UpdateTracksInBatches uses FindInBatches to iterate through a selection of Tracks in batches and updates each Track
// that it found.
func UpdateTracksInBatches(w io.Writer, db *gorm.DB) error {
	fmt.Fprintf(w, "Updating tracks")
	updated := 0
	if err := db.Transaction(func(tx *gorm.DB) error {
		var tracks []*Track
		return tx.Where("sample_rate > 44.1").FindInBatches(&tracks, 20, func(batchTx *gorm.DB, batch int) error {
			for _, track := range tracks {
				if track.SampleRate > 50 {
					track.SampleRate = track.SampleRate * 0.9
				} else {
					track.SampleRate = track.SampleRate * 0.95
				}
				if res := tx.Model(&track).Update("sample_rate", track.SampleRate); res.Error != nil || res.RowsAffected != int64(1) {
					if res.Error != nil {
						return res.Error
					}
					return fmt.Errorf("update of Track{%s,%v} affected %v rows", track.ID, track.TrackNumber, res.RowsAffected)
				}
				updated++
				fmt.Fprintf(w, ".")
			}
			return nil
		}).Error
	}); err != nil {
		return fmt.Errorf("failed to batch fetch and update tracks: %w", err)
	}
	fmt.Fprintf(w, "\nUpdated %v tracks\n\n", updated)
	return nil
}

func PrintAlbumsReleaseBefore1900(w io.Writer, db *gorm.DB) error {
	fmt.Println("Searching for albums released before 1900")
	var albums []*Album
	if err := db.Where(
		"release_date < ?",
		civil.DateOf(time.Date(1900, time.January, 1, 0, 0, 0, 0, time.UTC)),
	).Order("release_date asc").Find(&albums).Error; err != nil {
		return fmt.Errorf("failed to load albums: %w", err)
	}
	if len(albums) == 0 {
		fmt.Fprintf(w, "No albums found")
	} else {
		for _, album := range albums {
			fmt.Fprintf(w, "Album %q was released at %v\n", album.Title, album.ReleaseDate.String())
		}
	}
	fmt.Fprintf(w, "\n\n")
	return nil
}

func PrintSingersWithLimitAndOffset(w io.Writer, db *gorm.DB) error {
	fmt.Fprintf(w, "Printing all singers ordered by last name")
	var singers []*Singer
	limit := 5
	offset := 0
	for true {
		if err := db.Order("last_name, id").Limit(limit).Offset(offset).Find(&singers).Error; err != nil {
			return fmt.Errorf("failed to load singers at offset %v: %w", offset, err)
		}
		if len(singers) == 0 {
			break
		}
		for _, singer := range singers {
			fmt.Fprintf(w, "%v: %v\n", offset, singer.FullName)
			offset++
		}
	}
	fmt.Fprintf(w, "Found %v singers\n\n", offset)
	return nil
}

// QueryWithTimeout will try to execute a query with a 1ms timeout.
// This will normally cause a Deadline Exceeded error to be returned.
func QueryWithTimeout(w io.Writer, db *gorm.DB) error {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	var tracks []*Track
	if err := db.WithContext(ctx).Where("substring(title, 1, 1)='a'").Find(&tracks).Error; err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			fmt.Fprintf(w, "Query failed because of a timeout. This is expected.\n\n")
			return nil
		}
		return fmt.Errorf("query failed with an unexpected error, failed to load tracks: %w", err)
	}
	fmt.Fprintf(w, "Successfully queried all tracks in 1ms\n\n")
	return nil
}

func PrintAlbumsFirstCharTitleAndFirstOrLastNameEqual(w io.Writer, db *gorm.DB) error {
	fmt.Fprintf(w, "Searching for albums that have a title that starts with the same character as the first or last name of the singer")
	var albums []*Album
	// Join the Singer association to use it in the Where clause.
	// Note that `gorm` will use "Singer" (including quotes) as the alias for the singers table.
	// That means that all references to "Singer" in the query must be quoted, as PostgreSQL treats
	// the alias as case-sensitive.
	if err := db.Joins("Singer").Where(
		`LOWER(SUBSTR(albums.title, 1, 1)) = LOWER(SUBSTR(Singer.first_name, 1, 1))` +
			`OR LOWER(SUBSTR(albums.title, 1, 1)) = LOWER(SUBSTR(Singer.last_name, 1, 1))`,
	).Order(`Singer.last_name, albums.release_date asc`).Find(&albums).Error; err != nil {
		return fmt.Errorf("failed to load albums: %w", err)
	}
	if len(albums) == 0 {
		fmt.Fprintf(w, "No albums found that match the criteria")
	} else {
		for _, album := range albums {
			fmt.Fprintf(w, "Album %q was released by %v\n", album.Title, album.Singer.FullName)
		}
	}
	fmt.Fprintf(w, "\n\n")
	return nil
}

// SearchAlbumsUsingNamedArgument searches for Albums using a named argument.
func SearchAlbumsUsingNamedArgument(w io.Writer, db *gorm.DB, title string) error {
	fmt.Fprintf(w, "Searching for albums like %q\n", title)
	var albums []*Album
	if err := db.Where("title like @title", sql.Named("title", title)).Order("title").Find(&albums).Error; err != nil {
		return fmt.Errorf("failed to load albums: %w", err)
	}
	if len(albums) == 0 {
		fmt.Fprintf(w, "No albums found that match the criteria")
	} else {
		for _, album := range albums {
			fmt.Fprintf(w, "Album %q released at %v\n", album.Title, album.ReleaseDate.String())
		}
	}
	fmt.Fprintf(w, "\n\n")
	return nil
}

// CreateSinger creates a new Singer and stores in the database.
// Returns the ID of the Singer.
func CreateSinger(db *gorm.DB, firstName, lastName string) (int64, error) {
	singer := Singer{
		FirstName: sql.NullString{String: firstName, Valid: true},
		LastName:  lastName,
	}
	res := db.Create(&singer)
	// FullName is automatically generated by the database and should be returned to the client by
	// the insert statement.
	if singer.FullName != firstName+" "+lastName {
		return 0, fmt.Errorf("unexpected full name for singer: %v", singer.FullName)
	}
	return int64(singer.ID), res.Error
}

// CreateAlbumWithRandomTracks creates and stores a new Album in the database.
// Also generates numTracks random tracks for the Album.
// Returns the ID of the Album.
func CreateAlbumWithRandomTracks(db *gorm.DB, albumTitle string, singerId int64, numTracks int) (int64, error) {
	// We cannot include the Tracks that we want to create in the definition here, as gorm would then try to
	// use an UPSERT to save-or-update the album that we are creating. Instead, we need to create the album first,
	// and then create the tracks.
	album := &Album{
		Title:           albumTitle,
		MarketingBudget: sql.NullFloat64{Float64: randFloat64(0, 10000000)},
		ReleaseDate:     randDate(),
		SingerId:        int64(singerId),
		CoverPicture:    randBytes(randInt(5000, 15000)),
	}
	res := db.Create(album)
	if res.Error != nil {
		return 0, res.Error
	}
	tracks := make([]*Track, numTracks)
	for n := 0; n < numTracks; n++ {
		tracks[n] = &Track{Model: gorm.Model{ID: album.ID}, TrackNumber: int64(n + 1), Title: randTrackTitle(), SampleRate: randFloat64(30.0, 60.0)}
	}

	// Note: The batch size is deliberately kept small here in order to prevent the statement from getting too big and
	// exceeding the maximum number of parameters in a prepared statement. PGAdapter can currently handle at most 50
	// parameters in a prepared statement.
	res = db.CreateInBatches(tracks, 8)
	return int64(album.ID), res.Error
}

// DeleteRandomTrack will delete a randomly chosen Track from the database.
// This function shows how to delete a record with a primary key consisting of more than one column.
func DeleteRandomTrack(w io.Writer, db *gorm.DB) error {
	track := Track{}
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&track).Error; err != nil {
			return err
		}
		if track.ID == 0 {
			return fmt.Errorf("no track found")
		}
		if res := tx.Delete(&track); res.Error != nil || res.RowsAffected != int64(1) {
			if res.Error != nil {
				return res.Error
			}
			return fmt.Errorf("delete affected %v rows", res.RowsAffected)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("failed to delete a random track: %w", err)
	}
	fmt.Fprintf(w, "Deleted track %q (%q)\n\n", track.ID, track.Title)
	return nil
}

// DeleteRandomAlbum deletes a random Album. The Album could have one or more Tracks interleaved with it, but as the
// `INTERLEAVE IN PARENT` clause includes `ON DELETE CASCADE`, the child rows will be deleted along with the parent.
func DeleteRandomAlbum(w io.Writer, db *gorm.DB) error {
	album := Album{}
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&album).Error; err != nil {
			return fmt.Errorf("failed to load album: %w", err)
		}
		if album.ID == 0 {
			return fmt.Errorf("no album found")
		}
		// Note that the number of rows affected that is returned by Cloud Spanner excludes the number of child rows
		// that was deleted along with the parent row. This means that the number of rows affected should always be 1.
		if res := tx.Delete(&album); res.Error != nil || res.RowsAffected != int64(1) {
			if res.Error != nil {
				return res.Error
			}
			return fmt.Errorf("delete affected %v rows", res.RowsAffected)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("failed to delete a random album: %w", err)
	}
	fmt.Fprintf(w, "Deleted album %q (%q)\n\n", album.ID, album.Title)
	return nil
}

// CreateInterleavedTablesIfNotExist creates all tables that are required for this sample if they do not yet exist.
func CreateInterleavedTablesIfNotExist(w io.Writer, db *gorm.DB) error {
	fmt.Fprintf(w, "Creating tables...")
	ddlStatements := strings.FieldsFunc(string(createDataModelSQL), func(r rune) bool {
		return r == ';'
	})
	session := db.Session(&gorm.Session{SkipDefaultTransaction: true})
	for _, statement := range ddlStatements {
		if strings.TrimSpace(statement) == "" {
			continue
		}
		if err := session.Exec(statement).Error; err != nil {
			return fmt.Errorf("failed to execute statement: %w", err)
		}
	}
	fmt.Fprintf(w, "Finished creating interleaved tables")
	return nil
}

// DeleteAllData deletes all existing records in the database.
func DeleteAllData(db *gorm.DB) error {
	if err := db.Exec("DELETE FROM concerts WHERE 1=1").Error; err != nil {
		return err
	}
	if err := db.Exec("DELETE FROM venues WHERE 1=1").Error; err != nil {
		return err
	}
	if err := db.Exec("DELETE FROM tracks WHERE 1=1").Error; err != nil {
		return err
	}
	if err := db.Exec("DELETE FROM albums WHERE 1=1").Error; err != nil {
		return err
	}
	if err := db.Exec("DELETE FROM singers WHERE 1=1").Error; err != nil {
		return err
	}
	return nil
}

func randFloat64(min, max float64) float64 {
	return min + rnd.Float64()*(max-min)
}

func randInt(min, max int) int {
	return min + rnd.Int()%(max-min)
}

func randDate() spanner.NullDate {
	return spanner.NullDate{Date: civil.DateOf(time.Date(randInt(1850, 2010), time.Month(randInt(1, 12)), randInt(1, 28), 0, 0, 0, 0, time.UTC))}
}

func randBytes(length int) []byte {
	res := make([]byte, length)
	rnd.Read(res)
	return res
}

func randFirstName() string {
	return firstNames[randInt(0, len(firstNames))]
}

func randLastName() string {
	return lastNames[randInt(0, len(lastNames))]
}

func randAlbumTitle() string {
	return adjectives[randInt(0, len(adjectives))] + " " + nouns[randInt(0, len(nouns))]
}

func randTrackTitle() string {
	return adverbs[randInt(0, len(adverbs))] + " " + verbs[randInt(0, len(verbs))]
}

var firstNames = []string{
	"Saffron", "Eleanor", "Ann", "Salma", "Kiera", "Mariam", "Georgie", "Eden", "Carmen", "Darcie",
	"Antony", "Benjamin", "Donald", "Keaton", "Jared", "Simon", "Tanya", "Julian", "Eugene", "Laurence"}
var lastNames = []string{
	"Terry", "Ford", "Mills", "Connolly", "Newton", "Rodgers", "Austin", "Floyd", "Doherty", "Nguyen",
	"Chavez", "Crossley", "Silva", "George", "Baldwin", "Burns", "Russell", "Ramirez", "Hunter", "Fuller",
}
var adjectives = []string{
	"ultra",
	"happy",
	"emotional",
	"filthy",
	"charming",
	"alleged",
	"talented",
	"exotic",
	"lamentable",
	"lewd",
	"old-fashioned",
	"savory",
	"delicate",
	"willing",
	"habitual",
	"upset",
	"gainful",
	"nonchalant",
	"kind",
	"unruly",
}
var nouns = []string{
	"improvement",
	"control",
	"tennis",
	"gene",
	"department",
	"person",
	"awareness",
	"health",
	"development",
	"platform",
	"garbage",
	"suggestion",
	"agreement",
	"knowledge",
	"introduction",
	"recommendation",
	"driver",
	"elevator",
	"industry",
	"extent",
}
var verbs = []string{
	"instruct",
	"rescue",
	"disappear",
	"import",
	"inhibit",
	"accommodate",
	"dress",
	"describe",
	"mind",
	"strip",
	"crawl",
	"lower",
	"influence",
	"alter",
	"prove",
	"race",
	"label",
	"exhaust",
	"reach",
	"remove",
}
var adverbs = []string{
	"cautiously",
	"offensively",
	"immediately",
	"soon",
	"judgementally",
	"actually",
	"honestly",
	"slightly",
	"limply",
	"rigidly",
	"fast",
	"normally",
	"unnecessarily",
	"wildly",
	"unimpressively",
	"helplessly",
	"rightfully",
	"kiddingly",
	"early",
	"queasily",
}

func parseTimestamp(ts string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, ts)
	return t.UTC()
}
