package myutil

import (
	"encoding/base64"
	"fmt"
	"os"
)

//ファイルのbase64エンコード
func GetBase64String(filepath string) string {

	file, err := os.Open(filepath)
	if err != nil {
		fmt.Println(err)
		panic(err)
	}
	defer file.Close()

	fi, err := file.Stat() //FileInfo interface
	if err != nil {
		fmt.Println(err)
		panic(err)
	}
	size := fi.Size() //ファイルサイズ

	data := make([]byte, size)
	file.Read(data)

	return base64.StdEncoding.EncodeToString(data)
}
