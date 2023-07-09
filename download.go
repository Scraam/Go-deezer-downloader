package download

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"time"
)

const (
	// APIUrl is the deezer API
	APIUrl = "http://www.deezer.com/ajax/gw-light.php"
	// LoginURL is the API for deezer login
	LoginURL = "https://www.deezer.com/ajax/action.php"
	// deezer domain for cookie check
	Domain = "https://www.deezer.com"
)

func Download(id string, usertoken string) *bytes.Buffer {
	client, err := login(usertoken)
	if err != nil {
		log.Fatalf("%s: %v", err.Message, err.Error)
	}
	downloadURL, FName, client, err := getUrlDownload(id, client)
	if err != nil {
		log.Fatalf("%s: %v", err.Message, err.Error)
	}

	buf, err := getAudioFile(downloadURL, id, FName, client)
	if err != nil {
		log.Fatalf("%s and %v", err.Message, err.Error)
	}
	return buf
}

// Login will login the user with the provided credentials
func login(usertoken string) (*http.Client, *OnError) {
	CookieJar, _ := cookiejar.New(nil)
	client := &http.Client{
		Jar: CookieJar,
	}
	Deez := &DeezStruct{}
	req, err := newRequest(APIUrl, "POST", nil)
	args := []string{"null", "deezer.getUserData"}
	req = addQs(req, args...)
	resp, err := client.Do(req)
	body, _ := ioutil.ReadAll(resp.Body)
	err = json.Unmarshal(body, &Deez)
	if err != nil {
		return nil, &OnError{err, "Error during getCheckFormLogin Unmarshalling"}
	}

	CookieURL, _ := url.Parse(Domain)
	resp.Body.Close()

	form := url.Values{}
	form.Add("type", "login")
	form.Add("checkFormLogin", Deez.Results.CheckFormLogin)
	req, err = newRequest(LoginURL, "POST", form.Encode())
	if err != nil {
		return nil, &OnError{err, "Error during Login Request"}
	}

	req.Header.Set("Content-type", "application/x-www-form-urlencoded")
	req.Header.Add("Content-Length", strconv.Itoa(len(form.Encode())))

	resp, err = client.Do(req)
	if err != nil {
		return nil, &OnError{err, "Error during Login response"}
	}

	defer resp.Body.Close()
	body, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, &OnError{err, "Error During Login response Read Body"}
	}
	if resp.StatusCode == 200 {
		// Set the Cookie afte login succesfully
		addCookies(client, CookieURL, usertoken)
		return client, nil
	}
	return nil, &OnError{err,
		"Can't Login, resp status code is" + string(resp.StatusCode)}
}

func addCookies(client *http.Client, CookieURL *url.URL, usertoken string) {
	expire := time.Now().Add(time.Hour * 24 * 180)
	expire.Format("2006-01-02T15:04:05.999Z07:00")
	creation := time.Now().Format("2006-01-02T15:04:05.999Z07:00")
	lastUsed := time.Now().Format("2006-01-02T15:04:05.999Z07:00")
	rawcookie := fmt.Sprintf("arl=%s; expires=%v; %s creation=%v; lastAccessed=%v;",
		usertoken,
		expire,
		"path=/; domain=deezer.com; max-age=15552000; httponly=true; hostonly=false;",
		creation,
		lastUsed)
	cookies := []*http.Cookie{
		{
			Name:     "arl",
			Value:    usertoken,
			Expires:  expire,
			MaxAge:   15552000,
			Domain:   ".deezer.com",
			Path:     "/",
			HttpOnly: true,
			Raw:      rawcookie,
		},
	}

	client.Jar.SetCookies(CookieURL, cookies)

}

// getUrlDownload get the url for the requested track
func getUrlDownload(id string, client *http.Client) (string, string, *http.Client, *OnError) {
	// fmt.Println("Getting Download url")
	jsonTrack := &DeezTrack{}

	APIToken, _ := getToken(client)

	jsonPrep := `{"sng_id":"` + id + `"}`
	jsonStr := []byte(jsonPrep)
	req, err := newRequest(APIUrl, "POST", jsonStr)
	if err != nil {
		return "", "", nil, &OnError{err, "Error during GetUrlDownload request"}
	}

	qs := url.Values{}
	qs.Add("api_version", "1.0")
	qs.Add("api_token", APIToken)
	qs.Add("input", "3")
	qs.Add("method", "deezer.pageTrack")
	req.URL.RawQuery = qs.Encode()

	resp, _ := client.Do(req)
	body, _ := ioutil.ReadAll(resp.Body)
	defer resp.Body.Close()

	err = json.Unmarshal(body, &jsonTrack)
	if err != nil {
		return "", "", nil, &OnError{err, "Error during GetUrlDownload Unmarshalling"}
	}
	FileSize320, _ := jsonTrack.Results.DATA.FileSize320.Int64()
	FileSize256, _ := jsonTrack.Results.DATA.FileSize256.Int64()
	FileSize128, _ := jsonTrack.Results.DATA.FileSize128.Int64()
	var format string
	switch {
	case FileSize320 > 0:
		format = "3"
	case FileSize256 > 0:
		format = "5"
	case FileSize128 > 0:
		format = "1"
	default:
		format = "8"
	}
	songID := jsonTrack.Results.DATA.ID.String()
	md5Origin := jsonTrack.Results.DATA.MD5Origin
	mediaVersion := jsonTrack.Results.DATA.MediaVersion.String()
	songTitle := jsonTrack.Results.DATA.SngTitle
	artName := jsonTrack.Results.DATA.ArtName
	FName := fmt.Sprintf("%s - %s.mp3", songTitle, artName)

	downloadURL, err := decryptDownload(md5Origin, songID, format, mediaVersion)
	if err != nil {
		return "", "", nil, &OnError{err, "Error Getting DownloadUrl"}
	}

	return downloadURL, FName, client, nil
}

// getAudioFile gets the audio file from deezer server
func getAudioFile(downloadURL, id, FName string, client *http.Client) (*bytes.Buffer, *OnError) {
	// fmt.Println("Gopher's getting the audio File")
	req, err := newRequest(downloadURL, "GET", nil)
	if err != nil {
		return nil, &OnError{err, "Error during GetAudioFile Get request"}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, &OnError{err, "Error during GetAudioFile response"}
	}
	defer resp.Body.Close()
	buf, err := decryptMedia(resp.Body, id, FName, resp.ContentLength)
	if err != nil {
		return nil, &OnError{err, "Error during DecryptMedia"}
	}
	return buf, nil
}
