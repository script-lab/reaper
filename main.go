package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/joho/godotenv"
	"github.com/sclevine/agouti"
	"golang.org/x/net/context"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/sheets/v4"
)

func Create() {
	ctx := context.Background()

	jsonfile, err := ioutil.ReadFile("google_client_secret.json")
	if err != nil {
		log.Fatalf("google client secret を読み取れません: %v", err)
	}

	config, err := google.ConfigFromJSON(jsonfile, "https://www.googleapis.com/auth/spreadsheets", "https://www.googleapis.com/auth/drive.file")
	if err != nil {
		log.Fatalf("client secret fileを解析して構成できません: %v", err)
	}

	client := getClient(ctx, config)

	author, err := sheets.New(client)
	if err != nil {
		log.Fatalf("Sheetsクライアントを取得できません: %v", err)
	}

	newSheet := &sheets.Spreadsheet{
		Properties: &sheets.SpreadsheetProperties{
			Title:    "Scraping Spreadsheet",
			Locale:   "ja_JP",
			TimeZone: "Asia/Tokyo",
		},
	}

	createSheet, err := author.Spreadsheets.Create(newSheet).Context(ctx).Do()
	if err != nil {
		log.Fatalf("シートを作成できません: %v", err)
	}

	images := scraping()

	writeRange := "シート1"

	valueRange := &sheets.ValueRange{
		MajorDimension: "COLUMNS",
		Values: [][]interface{}{
			images,
		},
	}

	_, err = author.Spreadsheets.Values.Update(createSheet.SpreadsheetId, writeRange, valueRange).ValueInputOption("USER_ENTERED").Do()
	if err != nil {
		log.Fatalf("シートに書き込みができません: %v", err)
	}
}

func getClient(ctx context.Context, config *oauth2.Config) *http.Client {
	tokenFile := "token.json"

	token, err := tokenFromFile(tokenFile)
	if err != nil {
		token = getTokenFromWeb(config)
		saveToken(tokenFile, token)
	}

	return config.Client(context.Background(), token)
}

func getTokenFromWeb(config *oauth2.Config) *oauth2.Token {
	authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	fmt.Printf("ブラウザで次のリンクに移動し、次のように入力します"+
		"authorization code: \n%v\n", authURL)

	var authCode string
	if _, err := fmt.Scan(&authCode); err != nil {
		log.Fatalf("認証コードを読み取れません: %v", err)
	}

	tok, err := config.Exchange(context.TODO(), authCode)
	if err != nil {
		log.Fatalf("Webからトークンを取得できません: %v", err)
	}

	return tok
}

func saveToken(path string, token *oauth2.Token) {
	fmt.Printf("ファイルに保存: %s\n", path)

	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		log.Fatalf("oauthトークンをキャッシュできません: %v", err)
	}
	defer f.Close()

	json.NewEncoder(f).Encode(token)
}

func tokenFromFile(file string) (*oauth2.Token, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	token := &oauth2.Token{}
	err = json.NewDecoder(f).Decode(token)
	return token, err
}

func scraping() []interface{} {
	url := "https://www.instagram.com/"

	driver := agouti.ChromeDriver(agouti.Browser("chrome"))
	if err := driver.Start(); err != nil {
		log.Fatalf("ドライバーを起動できません: %v", err)
	}
	defer driver.Stop()

	page, err := driver.NewPage()
	if err != nil {
		log.Fatalf("ブラウザを開けません: %v", err)
	}

	if err := page.Navigate(url); err != nil {
		log.Fatalf("URLにアクセスできません: %v", err)
	}

	err := godotenv.Load()
	if err != nil {
		panic(err.Error())
	}

	accountName := os.Getenv("ACCOUNT_NAME")
	accountPass := os.Getenv("ACCOUNT_PASSWORD")
	userName := page.FindByName("username")
	password := page.FindByName("password")
	userName.Fill(accountName)
	password.Fill(accountPass)

	if err := page.FindByButton("ログイン").Submit(); err != nil {
		log.Fatalf("ログインできません: %v", err)
	}

	time.Sleep(5 * time.Second)

	if err := page.FindByButton("後で").Click(); err != nil {
		log.Fatalf("ログインできません: %v", err)
	}

	time.Sleep(1 * time.Second)

	if err := page.Navigate("https://www.instagram.com/explore/?hl=ja"); err != nil {
		log.Fatalf("URLにアクセスできません: %v", err)
	}

	images := []interface{}{}

	for i := 0; i <= 10; i++ {
		buf, err := page.HTML()
		if err != nil {
			log.Fatal(err)
		}

		reader := strings.NewReader(buf)

		doc, err := goquery.NewDocumentFromReader(reader)
		if err != nil {
			log.Fatal(err)
		}

		element := doc.Find("div.KL4Bh")
		selection := element.Find("img.FFVAD")

		valSlice := []interface{}{}

		selection.Each(func(_ int, value *goquery.Selection) {
			src, _ := value.Attr("src")
			valSlice = append(valSlice, src)
		})

		if valSlice == nil {
			panic("指定した値を取得できません")
		}

		images = append(images, valSlice...)

		selection.Remove()

		target := page.FindByClass("MxEZm")
		if err := target.ScrollFinger(0, 2000); err != nil {
			log.Fatalf("スクロールできません: %v", err)
		}
		page.SetImplicitWait(5)
		time.Sleep(2 * time.Second)
	}

	time.Sleep(1 * time.Second)

	return images
}

func main() {
	maxConnection := make(chan bool, 200)
	wg := &sync.WaitGroup{}

	count := 0
	start := time.Now()
	for maxRequest := 0; maxRequest < 1; maxRequest++ {
		wg.Add(1)
		maxConnection <- true
		go func() {
			defer wg.Done()

			Create()

			count++
			<-maxConnection
		}()
	}
	wg.Wait()
	end := time.Now()
	log.Printf("%d 回のリクエストに成功しました！\n", count)
	log.Printf("%f 秒処理に時間がかかりました！\n", (end.Sub(start)).Seconds())
}
