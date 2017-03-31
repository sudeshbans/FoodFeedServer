package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/gorilla/mux"
	"github.com/lib/pq"
)

const (
	DB_USER = ""
	DB_NAME = ""
	SERVER  = ""
	PATH    = ""
)

type databaseAccesor struct {
	db *sql.DB
}

type AWS_SESSION struct {
	svc *s3.S3
}

type Message struct {
	Result string `json:"result"`
	Status string `json:"status"`
}

type Feed struct {
	FOODID  int      `json:"foodid"`
	TITLE   string   `json:"title"`
	LIKES   int64    `json:"foodlike"`
	SAVES   int64    `json:"foodsave"`
	FBID    string   `json:"feedfbid"`
	CREATED int64    `json:"feedcreated"`
	FOODPIC string   `json:"foodPic"`
	PUBLIC  bool     `json:"public"`
	TAGS    []string `json:"tags"`
}

type UserFeed struct {
	Feed
	Profile
}

type Profile struct {
	ID      int    `json:"id"`
	NAME    string `json:"name"`
	FBID    string `json:"fbid"`
	PICTURE string `json:"picture"`
	CREATED int64  `json:"created"`
}

type Likes struct {
	FBID    string `json:"fbid"`
	FOODID  int    `json:"foodid"`
	CREATED int64  `json:"created"`
	STATE   bool   `json:"state"`
}

type Saves struct {
	FBID    string `json:"fbid"`
	FOODID  string `json:"foodid"`
	CREATED int64  `json:"created"`
}

func checkErr(err error) {
	if err != nil {
		fmt.Println("Error happened at")
		panic(err)
	}
}

func (dbWrapper *databaseAccesor) GetFeed(w http.ResponseWriter, req *http.Request) {
	var userFeed []UserFeed
	db := dbWrapper.db
	rows, err := db.Query("SELECT feed.id, feed.title, feed.likes, feed.tags, feed.created, feed.public, feed.foodpic, profile.name, profile.picture, profile.fbid FROM feed INNER JOIN profile ON (profile.fbid = feed.fbid) where feed.public = true order by created desc")

	checkErr(err)

	defer rows.Close()
	for rows.Next() {
		var title, foodpic, fbid string
		var name string
		var likes sql.NullInt64
		var picture string
		var tags []string
		var created int64
		var public bool
		var id int
		errorRow := rows.Scan(&id, &title, &likes, pq.Array(&tags), &created, &public, &foodpic, &name, &picture, &fbid)

		checkErr(errorRow)
		userFeed = append(userFeed, UserFeed{
			Feed{
				FOODID:  id,
				TITLE:   title,
				PUBLIC:  public,
				FOODPIC: foodpic,
				TAGS:    tags,
				LIKES:   likes.Int64,
				CREATED: created,
			},
			Profile{
				NAME:    name,
				PICTURE: picture,
				FBID:    fbid,
			},
		})
	}
	err = rows.Err() // get any error encountered during iteration

	checkErr(err)
	json.NewEncoder(w).Encode(&userFeed)
}

func (dbWrapper *databaseAccesor) GetProfile(w http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)
	fbid := vars["fbid"]
	var likes []int
	var saves []int
	db := dbWrapper.db
	rows, err := db.Query("SELECT foodid from likes where fbid = $1 and state = true;", fbid)
	checkErr(err)

	defer rows.Close()
	for rows.Next() {
		var likefoodid int
		errorRow := rows.Scan(&likefoodid)

		checkErr(errorRow)
		likes = append(likes, likefoodid)
	}
	err = rows.Err() // get any error encountered during iteration
	checkErr(err)

	saveRows, saveerr := db.Query("SELECT foodid from saves where fbid = $1 and state = true;", fbid)
	checkErr(saveerr)

	defer saveRows.Close()
	for saveRows.Next() {
		var savefoodid int
		errorRow := saveRows.Scan(&savefoodid)

		checkErr(errorRow)
		saves = append(saves, savefoodid)
	}
	err = saveRows.Err()
	checkErr(err)

	type PROFILE struct {
		LIKED []int `json:"liked"`
		SAVED []int `json:"saved"`
	}

	profile := PROFILE{LIKED: likes, SAVED: saves}
	json.NewEncoder(w).Encode(&profile)
}

func (dbWrapper *databaseAccesor) AddLike(w http.ResponseWriter, req *http.Request, vars map[string]string) {
	var db = dbWrapper.db
	var like Likes

	like.CREATED = time.Now().Unix()
	like.FBID = vars["fbid"]
	like.FOODID, _ = strconv.Atoi(vars["foodid"])
	like.STATE, _ = strconv.ParseBool(vars["state"])

	stmt, statementError := db.Prepare("INSERT INTO likes(fbid, foodid, created, state) VALUES($1, $2, $3, $4);")

	checkErr(statementError)

	_, eroorCheck := stmt.Exec(like.FBID, like.FOODID, like.CREATED, like.STATE)

	checkErr(eroorCheck)

	stmt, err := db.Prepare("UPDATE FEED SET likes = likes + 1 where id=$1")
	checkErr(err)

	_, feedErr := stmt.Exec(like.FOODID)
	checkErr(feedErr)

	m := Message{"ok", "200"}
	json.NewEncoder(w).Encode(&m)
}

func (dbWrapper *databaseAccesor) AddSave(w http.ResponseWriter, req *http.Request, vars map[string]string) {
	var db = dbWrapper.db
	var save Likes

	save.CREATED = time.Now().Unix()
	save.FBID = vars["fbid"]
	save.FOODID, _ = strconv.Atoi(vars["foodid"])
	save.STATE, _ = strconv.ParseBool(vars["state"])

	stmt, statementError := db.Prepare("INSERT INTO saves(fbid, foodid, created, state) VALUES($1, $2, $3, $4);")

	checkErr(statementError)

	_, eroorCheck := stmt.Exec(save.FBID, save.FOODID, save.CREATED, save.STATE)

	checkErr(eroorCheck)

	stmt, err := db.Prepare("UPDATE FEED SET saves = saves + 1 where id=$1")
	checkErr(err)

	_, feedErr := stmt.Exec(save.FOODID)
	checkErr(feedErr)

	m := Message{"ok", "200"}
	json.NewEncoder(w).Encode(&m)
}

func (dbWrapper *databaseAccesor) UpdateLike(w http.ResponseWriter, req *http.Request, vars map[string]string) {

	var db = dbWrapper.db
	var like Likes

	like.CREATED = time.Now().Unix()
	like.FBID = vars["fbid"]
	like.FOODID, _ = strconv.Atoi(vars["foodid"])
	like.STATE, _ = strconv.ParseBool(vars["state"])

	stmt, statementError := db.Prepare("UPDATE Likes SET state = $1 where foodid = $2 and fbid = $3")

	checkErr(statementError)

	_, eroorCheck := stmt.Exec(like.STATE, like.FOODID, like.FBID)

	checkErr(eroorCheck)

	stmt, err := db.Prepare("UPDATE FEED SET likes = likes + 1 where id=$1")
	checkErr(err)

	_, feedErr := stmt.Exec(like.FOODID)
	checkErr(feedErr)

	m := Message{"ok", "200"}
	json.NewEncoder(w).Encode(&m)
}

func (dbWrapper *databaseAccesor) UpdateSave(w http.ResponseWriter, req *http.Request, vars map[string]string) {

	var db = dbWrapper.db
	var save Likes

	save.CREATED = time.Now().Unix()
	save.FBID = vars["fbid"]
	save.FOODID, _ = strconv.Atoi(vars["foodid"])
	save.STATE, _ = strconv.ParseBool(vars["state"])

	stmt, statementError := db.Prepare("UPDATE Saves SET state = $1 where foodid = $2 and fbid = $3")

	checkErr(statementError)

	_, eroorCheck := stmt.Exec(save.STATE, save.FOODID, save.FBID)

	checkErr(eroorCheck)

	stmt, err := db.Prepare("UPDATE FEED SET saves = saves + 1 where id=$1")
	checkErr(err)

	_, feedErr := stmt.Exec(save.FOODID)
	checkErr(feedErr)

	m := Message{"ok", "200"}
	json.NewEncoder(w).Encode(&m)
}

func (dbWrapper *databaseAccesor) Saved(w http.ResponseWriter, req *http.Request) {
	var userFeed []UserFeed
	db := dbWrapper.db
	vars := mux.Vars(req)
	fbid := vars["id"]
	rows, err := db.Query("Select  feed.id, feed.title, feed.likes, feed.tags, feed.created, feed.public, feed.foodpic, profile.name, profile.picture from feed inner join profile on profile.fbid = feed.fbid inner join saves on saves.foodid = feed.id where saves.fbid = $1 and saves.state = true;", &fbid)
	checkErr(err)

	defer rows.Close()
	for rows.Next() {
		var title, foodpic string
		var name string
		var likes sql.NullInt64
		var picture string
		var tags []string
		var created int64
		var public bool
		var id int
		errorRow := rows.Scan(&id, &title, &likes, pq.Array(&tags), &created, &public, &foodpic, &name, &picture)

		checkErr(errorRow)
		userFeed = append(userFeed, UserFeed{
			Feed{
				FOODID:  id,
				TITLE:   title,
				PUBLIC:  public,
				FOODPIC: foodpic,
				TAGS:    tags,
				LIKES:   likes.Int64,
				CREATED: created,
			},
			Profile{
				NAME:    name,
				PICTURE: picture,
			},
		})
	}
	err = rows.Err() // get any error encountered during iteration

	checkErr(err)
	json.NewEncoder(w).Encode(&userFeed)
}

func (dbWrapper *databaseAccesor) AddFood(w http.ResponseWriter, req *http.Request) {
	var db = dbWrapper.db
	var newFood Feed
	var userFeed UserFeed

	decoder := json.NewDecoder(req.Body)
	err := decoder.Decode(&newFood)
	checkErr(err)

	defer req.Body.Close()

	newFood.CREATED = time.Now().Unix()

	var foodid int
	statementError := db.QueryRow("INSERT INTO feed(title, public, fbid, created, foodpic, tags, saves ) VALUES($1, $2, $3, $4, $5, $6, $7) Returning id", newFood.TITLE, newFood.PUBLIC, newFood.FBID, newFood.CREATED, newFood.FOODPIC, pq.Array(newFood.TAGS), 1).Scan(&foodid)

	checkErr(statementError)

	stmt, statementError := db.Prepare("INSERT INTO saves(fbid, foodid, created, state) VALUES($1, $2, $3, $4);")

	checkErr(statementError)

	_, _ = stmt.Exec(newFood.FBID, foodid, newFood.CREATED, true)

	userFeed = UserFeed{
		Feed{
			FOODID:  foodid,
			TITLE:   newFood.TITLE,
			PUBLIC:  newFood.PUBLIC,
			FOODPIC: newFood.FOODPIC,
			TAGS:    newFood.TAGS,
			LIKES:   0,
			CREATED: newFood.CREATED,
		},
		Profile{},
	}

	json.NewEncoder(w).Encode(&userFeed)
}

func (dbWrapper *databaseAccesor) CreateProfile(w http.ResponseWriter, req *http.Request) {
	var db = dbWrapper.db
	var profile Profile
	var fbid string
	decoder := json.NewDecoder(req.Body)
	err := decoder.Decode(&profile)
	checkErr(err)
	defer req.Body.Close()

	profile.CREATED = time.Now().Unix()

	if profile.NAME == "Foods Feeds" {
		profile.NAME = "Food Feed"
	}

	rowError := db.QueryRow("Select fbid from profile where fbid = $1", profile.FBID).Scan(&fbid)

	if rowError == sql.ErrNoRows {
		print("i am never here")
		stmt, _ := db.Prepare("INSERT INTO profile(name, fbid, picture, created) VALUES($1, $2, $3, $4);")
		stmt.Exec(profile.NAME, profile.FBID, profile.PICTURE,
			profile.CREATED)
	} else if rowError != nil {
		print("i am never here")
		checkErr(rowError)
	}

	m := Message{"ok", "200"}
	json.NewEncoder(w).Encode(&m)
}

func (sessionWrapper *AWS_SESSION) UploadHandler(w http.ResponseWriter, r *http.Request) {
	file, header, err := r.FormFile("file")
	checkErr(err)
	defer file.Close()

	unixEpoch := time.Now().Unix()
	contentType := header.Header.Get("Content-Type")
	fileTypeString := strings.Split(contentType, "/")
	contentDisposition := header.Header.Get("Content-Disposition")
	_, params, _ := mime.ParseMediaType(contentDisposition)
	userid := params["filename"]
	// user ID and time
	filename := fmt.Sprintf(`%s%s-%d.%s`, PATH, userid, unixEpoch, fileTypeString[1])
	f, err := os.Create(filename)

	checkErr(err)
	defer f.Close()
	_, _ = io.Copy(f, file)

	// production http://138.68.226.145:6464/
	savedLocation := fmt.Sprintf("%s%s-%d.%s", SERVER, userid, unixEpoch, fileTypeString[1])

	message := Message{savedLocation, "200"}
	json.NewEncoder(w).Encode(&message)
	// Write to AWS
	// cur, err := file.Seek(0, 1)
	// size, err := file.Seek(0, 2)
	// _, _ = file.Seek(cur, 0)
	//
	// session := sessionWrapper.svc
	// buffer := make([]byte, size)
	//
	// file.Read(buffer)
	//
	// fileBytes := bytes.NewReader(buffer)
	// fileType := http.DetectContentType(buffer)
	// path := fmt.Sprintf("/media/%s", header.Filename)
	//
	// params := &s3.PutObjectInput{
	// 	Bucket:      aws.String("food2017"),
	// 	Key:         aws.String(path),
	// 	ACL:         aws.String("public-read"),
	// 	Body:        fileBytes,
	// 	ContentType: &fileType,
	// }
	// resp, err := session.PutObject(params)
	// checkErr(err)
	// fmt.Printf("response %s", awsutil.StringValue(resp))
}

func (dbWrapper *databaseAccesor) FindLike(w http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)

	var db = dbWrapper.db
	var like Likes
	var foodid int

	like.FBID = vars["fbid"]
	like.FOODID, _ = strconv.Atoi(vars["foodid"])
	like.STATE, _ = strconv.ParseBool(vars["state"])

	rowError := db.QueryRow("Select foodid from likes where fbid = $1 and foodid = $2", like.FBID, like.FOODID).Scan(&foodid)

	if rowError == sql.ErrNoRows {
		dbWrapper.AddLike(w, req, vars)
	} else if rowError != nil {
		checkErr(rowError)
	} else {
		dbWrapper.UpdateLike(w, req, vars)
	}
}

func (dbWrapper *databaseAccesor) FindSave(w http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)

	var db = dbWrapper.db
	var save Likes
	var foodid int

	save.FBID = vars["fbid"]
	save.FOODID, _ = strconv.Atoi(vars["foodid"])
	save.STATE, _ = strconv.ParseBool(vars["state"])

	rowError := db.QueryRow("Select foodid from saves where fbid = $1 and foodid = $2", save.FBID, save.FOODID).Scan(&foodid)

	if rowError == sql.ErrNoRows {
		dbWrapper.AddSave(w, req, vars)
	} else if rowError != nil {
		checkErr(rowError)
	} else {
		dbWrapper.UpdateSave(w, req, vars)
	}
}

func main() {
	dbinfo := fmt.Sprintf("user=%s dbname=%s sslmode=disable", DB_USER,
		DB_NAME)
	aws_access_key_id := ""
	aws_secret_access_key := ""
	token := ""
	creds := credentials.NewStaticCredentials(aws_access_key_id, aws_secret_access_key, token)
	_, err := creds.Get()

	checkErr(err)

	cfg := aws.NewConfig().WithRegion("aws-region-here").WithCredentials(creds)
	svc := s3.New(session.New(), cfg)

	sessionWrapper := &AWS_SESSION{svc: svc}

	db, err := sql.Open("postgres", dbinfo)
	checkErr(err)

	dbWrapper := &databaseAccesor{db: db}

	defer dbWrapper.db.Close()

	err = dbWrapper.db.Ping()
	checkErr(err)

	fmt.Println("connected successfully")

	var router = mux.NewRouter()

	router.HandleFunc("/feed", dbWrapper.GetFeed).Methods("GET")
	router.HandleFunc("/feed", dbWrapper.AddFood).Methods("POST")
	router.HandleFunc("/profile", dbWrapper.CreateProfile).Methods("POST")
	router.HandleFunc("/profile/{fbid}", dbWrapper.GetProfile).Methods("GET")
	router.HandleFunc("/upload", sessionWrapper.UploadHandler).Methods("POST")
	router.HandleFunc("/like/{foodid}/{fbid}/{state}", dbWrapper.FindLike).Methods("GET")
	router.HandleFunc("/save/{foodid}/{fbid}/{state}", dbWrapper.FindSave).Methods("GET")
	router.HandleFunc("/saved/{id}", dbWrapper.Saved).Methods("GET")
	router.PathPrefix("/").Handler(http.FileServer(http.Dir("./pics/")))

	http.ListenAndServe(":6464", router)
}
