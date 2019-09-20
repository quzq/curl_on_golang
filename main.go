package main

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/google/uuid"
	"github.com/kyamyam/curl_test/myutil"
)

type Config struct {
	Server ServerConfig `toml:"server"`
}

type ServerConfig struct {
	Host   string `toml:"host"`
	Tenant string `toml:"tenant"`
	User   string `toml:"user"`
	Pass   string `toml:"pass"`
}

/************************************************************
 * メイン処理
 */
func main() {
	//	例外(panic())のハンドリング。recover()は例外で投げられた値を捕捉する。
	defer func() {
		if e := recover(); e != nil {
			log.Fatal("Caught error: ", e)
		}
	}()

	var config Config
	_, err := toml.DecodeFile("config.tml", &config)
	if err != nil {
		panic(err)
	}

	base64 := myutil.GetBase64String("resource/216kb.pdf")

	wg := sync.WaitGroup{} // 非同期処理の待機グループを作成

	// ファイルアップロード
	{
		_ = base64
		base64 := myutil.GetBase64String("resource/simple.pdf")
		var token = getToken(config.Server.Host, config.Server.Tenant, config.Server.User, config.Server.Pass)
		times := 2
		for j := 0; j < 60*60; j++ {
			for i := 0; i < times; i++ {
				wg.Add(1)
				go fileUpload(i+j*times, config.Server.Host, token, base64, &wg)
			}
			time.Sleep(1 * time.Second) // 1秒休む
		}
	}

	// テストパターン 4APIを１シーケンスとして、１分間に最低125シーケンスを実行し、レスポンスエラーのないことを確認する。
	// {
	// 	times := 2
	// 	for j := 0; j < 60; j++ {
	// 		// if j%10 == 0 {
	// 		// 	wg.Add(1)
	// 		// 	go api_seq_success(1000+j, config.Server.Host, config.Server.Tenant, config.Server.User, config.Server.Pass, base64, &wg)
	// 		// 	//go api_seq_failed(1000+j, config.Server.Host, config.Server.Tenant, config.Server.User, config.Server.Pass, base64, &wg)
	// 		// }
	// 		for i := 0; i < times; i++ {
	// 			wg.Add(1)
	// 			// go文のついた関数はgoroutineとなり、並行（非同期）処理される。
	// 			go api_seq_success(i+j*times, config.Server.Host, config.Server.Tenant, config.Server.User, config.Server.Pass, base64, &wg)
	// 			//go api_seq_failed(i+j*times, config.Server.Host, config.Server.Tenant, config.Server.User, config.Server.Pass, base64, &wg)
	// 		}
	// 		time.Sleep(1 * time.Second) // 1秒休む
	// 	}
	// }
	// テストパターン 4APIを１シーケンスとして、３０秒に１度、５０成功シーケンスを並列に呼び出す。これを９０秒。レスポンスエラーのないことを確認する。
	// {
	// 	times := 50
	// 	for j := 0; j < 3; j++ {
	// 		for i := 0; i < times; i++ {
	// 			wg.Add(1)
	// 			// go文のついた関数はgoroutineとなり、並行（非同期）処理される。
	// 			go api_seq_success(i+j*times, config.Server.Host, config.Server.Tenant, config.Server.User, config.Server.Pass, base64, &wg)
	// 			//go api_seq_failed(i+j*times, config.Server.Host, config.Server.Tenant, config.Server.User, config.Server.Pass, base64, &wg)
	// 		}
	// 		time.Sleep(30 * time.Second) // 30秒休む
	// 	}
	// }

	wg.Wait() // 非同期処理の完了まで待つ
}

// 成功時のAPI呼び出しシーケンス
func api_seq_success(no int, host string, tenant string, user string, pass string, base64 string, wg *sync.WaitGroup) {
	defer func() {
		// if e := recover(); e != nil {
		// 	log.Fatal(no, "api_seq_success caught error: ", e)
		// 	panic(e)
		// } else {
		// 	fmt.Println(no, "done.")
		// }
		fmt.Println(no, "done.")
		wg.Done()
	}()
	var token = getToken(host, tenant, user, pass)
	var fileId = uploadFile(host, token, base64)
	//fmt.Println(no, fileId)
	status := stamp(host, token, fileId)
	_ = status // blank identifier (_)を用いて、not use を避ける。 golangでは、not useを認めない
	//fmt.Println(no, status)
	download(host, token, fileId)
}

// 失敗時のAPI呼び出しシーケンス
func api_seq_failed(no int, host string, tenant string, user string, pass string, base64 string, wg *sync.WaitGroup) {
	defer func() {
		fmt.Println(no, "done.")
		wg.Done()
	}()
	var token = getToken(host, tenant, user, pass)
	var fileId = uploadFile(host, token, base64)

	stamp(host, token, fileId)
	deleteFile(host, token, fileId)
}

// ファイルアップロード専用
func fileUpload(no int, host string, token string, base64 string, wg *sync.WaitGroup) {
	defer func() {
		fmt.Println(no, "done.")
		wg.Done()
	}()
	var fileId = uploadFile(host, token, base64)
	//fmt.Println(no, fileId)
	status := stamp(host, token, fileId)
	_ = status // blank identifier (_)を用いて、not use を避ける。 golangでは、not useを認めない
}

type ResponseErrors struct {
	Dummy string `json:"dummy"`
}

type ResponseStatus struct {
	Success bool             `json:"success"`
	Errors  []ResponseErrors `json:"errors"`
}

// 認証APIのコール
func getToken(host string, tenant string, user string, pass string) string {

	// map: 連想配列のようなもの。key-value型データ。map[keyのデータ型]valueのデータ型{key:valueのリスト} で定義される。
	payload := map[string]string{
		"tenant_name":  tenant,
		"account_name": user,
		"password":     pass,
	}

	// Go Objectをjson文字列にencode
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		fmt.Println(err)
		panic(err)
	}
	reqbody := bytes.NewReader(payloadBytes) //BytesReaderObject生成用の関数。Goにコンストラクタは存在せず、生成用の関数を準備する。関数の名前は New+struct名(+付加情報)にする。

	req, err := http.NewRequest("POST", host+"/api/login", reqbody)
	if err != nil {
		fmt.Println(err)
		panic(err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Println(err)
		panic(err)
	}

	defer resp.Body.Close() //deferの後に、カレントの関数が終了する際に実行すべき処理を記述する。複数行にわたる処理を同時に渡したい場合は、即時関数を渡す。複数のdeferを定義して処理をスタックすることも可能。try{}catch()のfinally的な処理をしたいときに使う。

	resbody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Println(err)
		panic(err)
	}

	type ResponseBody struct {
		Token string `json:"token"`
	}

	type HttpResponse struct {
		Status ResponseStatus `json:"status"`
		Body   ResponseBody   `json:"body"`
	}

	var p HttpResponse

	err = json.Unmarshal(resbody, &p) //JSON(in bytes)をGo Objectに変換する
	if err != nil {
		fmt.Println(err)
		panic(err)
	}

	return p.Body.Token
}

// ファイルアップロードAPIのコール
func uploadFile(host string, token string, base64 string) string {
	u, err := uuid.NewRandom()
	if err != nil {
		fmt.Println(err)
		panic(err)
	}

	dataUriString := "data:application/pdf;base64," + base64
	//dataUriStringよりmd5ハッシュを作成
	hasher := md5.New()
	hasher.Write([]byte(dataUriString))
	checksum := hex.EncodeToString(hasher.Sum(nil))

	jsonStr := []byte(`{"dir_id":null,"files":[{"name":"` + u.String() + `.pdf","mime_type":"application/pdf","size":3028,"base64":"` + dataUriString + `","checksum":"` + checksum + `","authorities":[],"tags":[]}]}`)

	req, err := http.NewRequest("POST", host+"/api/v1/files", bytes.NewBuffer(jsonStr))
	if err != nil {
		fmt.Println(err)
		panic(err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Auth-Cloud-Storage", token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Println(err)
		panic(err)
	}
	defer resp.Body.Close() //関数終了時にresp.Bodyをclose

	resbody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Println(err)
		panic(err)
	}

	type ResponseBody struct {
		Id string `json:"_id"`
	}

	type HttpResponse struct {
		Status ResponseStatus `json:"status"`
		Body   []ResponseBody `json:"body"`
	}

	var p HttpResponse

	err = json.Unmarshal(resbody, &p) //JSON(in bytes)をGo Objectに変換する
	if err != nil {
		fmt.Println(err, string(resbody))
		panic(err)
	}
	if !p.Status.Success {
		//fmt.Println(string(resbody)) //生のjsonをそのまま出力
		panic(string(resbody))
	}

	return p.Body[0].Id
}
func stamp(host string, token string, fileId string) bool {

	req, err := http.NewRequest("POST", host+"/api/v1/files/"+fileId+"/timestamp/grant", nil)
	if err != nil {
		fmt.Println(err)
		panic(err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Auth-Cloud-Storage", token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Println(err)
		panic(err)
	}
	defer resp.Body.Close() //関数終了時にresp.Bodyをclose

	resbody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Println(err)
		panic(err)
	}

	type ResponseStatus struct {
		Success bool           `json:"success"`
		Errors  ResponseErrors `json:"errors"`
	}

	type HttpResponse struct {
		Status ResponseStatus `json:"status"`
	}

	var p HttpResponse

	err = json.Unmarshal(resbody, &p)
	if err != nil {
		fmt.Println(err, string(resbody))
		panic(err)
	}

	if !p.Status.Success {
		//fmt.Println(string(resbody)) //生のjsonをそのまま出力
		panic(string(resbody))
	}
	return p.Status.Success

}

// ダウンロードAPIのコール
func download(host string, token string, fileId string) {
	req, err := http.NewRequest("GET", host+"/api/v1/files/download?file_id="+fileId, nil)
	if err != nil {
		// handle err
		panic(err)
	}
	req.Header.Set("Accept", "application/pdf")
	req.Header.Set("X-Auth-Cloud-Storage", token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		// handle err
		panic(err)
	}
	defer resp.Body.Close()

	resbody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Println(err)
		panic(err)
	}
	_ = resbody
}

// ファイル削除（to ゴミ箱）APIのコール
func deleteFile(host string, token string, fileId string) bool {

	req, err := http.NewRequest("DELETE", host+"/api/v1/files/"+fileId, nil)
	if err != nil {
		panic(err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Auth-Cloud-Storage", token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	resbody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Println(err)
		panic(err)
	}

	type ResponseStatus struct {
		Success bool           `json:"success"`
		Errors  ResponseErrors `json:"errors"`
	}

	type HttpResponse struct {
		Status ResponseStatus `json:"status"`
	}

	var p HttpResponse

	err = json.Unmarshal(resbody, &p)
	if err != nil {
		fmt.Println(err, string(resbody))
		panic(err)
	}

	if !p.Status.Success {
		//fmt.Println(string(resbody)) //生のjsonをそのまま出力
		panic(string(resbody))
	}
	return p.Status.Success

}
