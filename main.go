package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"time"
)

type Data struct {
	P string      `json:"p,omitempty"`
	O string      `json:"o,omitempty"`
	V interface{} `json:"v,omitempty"`
}

type ResponseData struct {
	Data struct {
		List []struct {
			CarID  string `json:"carID"`
			Status int    `json:"status"`
		} `json:"list"`
	} `json:"data"`
}

func main() {

	var userMessage string
	if len(os.Args) > 1 {
		userMessage = os.Args[1]
	}
	url := "https://.cn/carpage"
	headers := map[string]string{
		"Content-Type": "application/json",
		"Accept":       "application/json, text/plain, */*",
		"Origin":       "https://.cn",
		"Referer":      "https://.cn/list",
		"User-Agent":   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	}

	data := map[string]interface{}{"page": 1, "size": 50, "sort": "desc", "order": "sort"}
	jsonData, err := json.Marshal(data)
	if err != nil {
		log.Fatalf("Error marshalling data: %v", err)
	}

	client := &http.Client{}
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		log.Fatalf("Error creating request: %v", err)
	}

	for key, value := range headers {
		req.Header.Set(key, value)
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Fatalf("Error sending request: %v", err)
	}
	defer resp.Body.Close()

	var responseData ResponseData
	if err := json.NewDecoder(resp.Body).Decode(&responseData); err != nil {
		log.Fatalf("Error decoding response: %v", err)
	}

	var activeCars []struct {
		CarID  string `json:"carID"`
		Status int    `json:"status"`
	}
	for _, car := range responseData.Data.List {
		if car.Status == 1 {
			activeCars = append(activeCars, car)
		}
	}

	if len(activeCars) > 0 {
		rand.Seed(time.Now().UnixNano())
		randomCar := activeCars[rand.Intn(len(activeCars))]
		newURL := fmt.Sprintf("https://.cn/auth/login?carid=%s", randomCar.CarID)
		config(newURL, userMessage)
	} else {
		log.Println("没有找到符合条件的车队")
	}
}

type Config struct {
	UserToken       string `json:"usertoken"`
	Cookie          string `json:"cookie"`
	ParentMessageID string `json:"parentMessageID"`
	ConversationID  string `json:"conversationID"`
}

func config(url string, userMessage string) {
	// 读取配置文件
	configFile, err := os.Open("config.json")
	if err != nil {
		log.Fatalf("打开配置文件失败: %v", err)
	}
	defer configFile.Close()
	var config Config
	if err := json.NewDecoder(configFile).Decode(&config); err != nil {
		log.Fatalf("解析配置文件失败: %v", err)
	}

	// 如果config.UserToken为空就退出
	if config.UserToken == "" {
		log.Fatalf("UserToken为空，退出程序")
		os.Exit(1)
	}

	if config.Cookie == "" {

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		allocatorCtx, cancel := chromedp.NewRemoteAllocator(ctx, "ws://:9222")
		defer cancel()
		ctx, cancel = chromedp.NewContext(allocatorCtx)
		defer cancel()

		err = chromedp.Run(ctx,
			chromedp.ActionFunc(func(ctx context.Context) error {
				if err := network.ClearBrowserCookies().Do(ctx); err != nil {
					log.Printf("清空Cookie失败 - %v", err)
				}
				return nil
			}),
			chromedp.Navigate(url),
			chromedp.WaitVisible("input[name='usertoken']", chromedp.ByQuery),
			chromedp.SendKeys("input[name='usertoken']", config.UserToken, chromedp.ByQuery),
			chromedp.Click("button[type='submit'][name='action']", chromedp.ByQuery),
			chromedp.WaitVisible("body", chromedp.ByQuery),
			chromedp.Sleep(5*time.Second),
		)
		if err != nil {
			log.Fatalf("浏览器操作失败: %v", err)
		}

		var cookies string
		err = chromedp.Run(ctx, chromedp.Evaluate(`document.cookie`, &cookies))
		if err != nil {
			log.Fatalf("找不到cookie: %v", err)
		}

		// 更新cookie到配置文件
		config.Cookie = cookies
		updateConfigFile(config)
		navigateToURL(config.UserToken, config.Cookie, config.ConversationID, config.ParentMessageID, userMessage)
	} else {
		navigateToURL(config.UserToken, config.Cookie, config.ConversationID, config.ParentMessageID, userMessage)
	}
}

func updateConfigFile(config Config) {
	configFile, err := os.Create("config.json")
	if err != nil {
		log.Fatalf("打开配置文件失败: %v", err)
	}
	defer configFile.Close()
	if err := json.NewEncoder(configFile).Encode(config); err != nil {
		log.Fatalf("写入配置文件失败: %v", err)
	}
}

func navigateToURL(usertoken string, cookies string, ConversationID string, ParentMessageID string, userMessage string) {
	url := "https://.cn/backend-api/conversation"
	headers := map[string]string{
		"Accept":        "text/event-stream",
		"Authorization": "Bearer " + usertoken,
		"Content-Type":  "application/json",
		"Cookie":        cookies,
		"Origin":        "https://.cn",
		"Referer":       "https://.cn/?model=auto",
	}

	requestBody := map[string]interface{}{
		"action": "next",
		"messages": []map[string]interface{}{
			{
				"author": map[string]string{
					"role": "user",
				},
				"content": map[string]interface{}{
					"content_type": "text",
					"parts":        []string{userMessage},
				},
			},
		},
		"conversation_id":   ConversationID,
		"parent_message_id": ParentMessageID,
		"model":             "auto",
	}

	jsonData, _ := json.Marshal(requestBody)
	req, _ := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	client := &http.Client{}
	resp, _ := client.Do(req)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		clearCookieInConfig()
		main()
		return
	}
	// 处理响应内容
	processedContent := processResponse(string(body))
	// 继续进行后续操作
	response(processedContent)

}

func processResponse(response string) string {
	var result strings.Builder
	scanner := bufio.NewScanner(strings.NewReader(response))
	var current *Data

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "event:") ||
			line == `data: "v1"` ||
			line == `data: [DONE]` ||
			strings.TrimSpace(line) == "" {
			continue
		}

		if strings.HasPrefix(line, "data:") {
			var d Data
			jsonStr := strings.TrimPrefix(line, "data: ")
			if err := json.Unmarshal([]byte(jsonStr), &d); err != nil {
				result.WriteString(line + "\n")
				continue
			}

			switch {
			case d.P != "" && d.O != "":
				if current != nil {
					writeData(&result, current)
				}
				if vs, ok := d.V.(string); ok {
					current = &Data{P: d.P, O: d.O, V: vs}
				} else {
					result.WriteString(line + "\n")
					current = nil
				}

			case d.V != nil && d.P == "" && d.O == "":
				if current != nil {
					if vs, ok := d.V.(string); ok {
						current.V = current.V.(string) + vs
					} else {
						writeData(&result, current)
						result.WriteString(line + "\n")
						current = nil
					}
				} else {
					result.WriteString(line + "\n")
				}

			default:
				if current != nil {
					writeData(&result, current)
					current = nil
				}
				result.WriteString(line + "\n")
			}
		} else {
			result.WriteString(line + "\n")
		}
	}

	if current != nil {
		writeData(&result, current)
	}

	return result.String()
}

func writeData(writer *strings.Builder, data *Data) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		fmt.Println("JSON序列化错误:", err)
		return
	}
	writer.WriteString(fmt.Sprintf("data:%s\n", jsonData))
}

func response(processedContent string) {
	_, messageID := extractConversationAndMessageID(processedContent)

	// 更新配置文件
	configFile, err := os.Open("config.json")
	if err != nil {
		log.Fatalf("打开配置文件失败: %v", err)
	}
	defer configFile.Close()
	var config Config
	if err := json.NewDecoder(configFile).Decode(&config); err != nil {
		log.Fatalf("解析配置文件失败: %v", err)
	}

	config.ParentMessageID = messageID
	updateConfigFile(config)

	// 提取回复内容
	reply := extractReplyContent(processedContent)
	fmt.Printf(" %s\n", reply)
}

func extractConversationAndMessageID(content string) (string, string) {
	var conversationID, messageID string

	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data:") {
			var data map[string]interface{}
			jsonStr := strings.TrimPrefix(line, "data: ")
			if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
				continue
			}

			if v, ok := data["v"].(map[string]interface{}); ok {
				if msg, ok := v["message"].(map[string]interface{}); ok {
					if author, ok := msg["author"].(map[string]interface{}); ok {
						if role, ok := author["role"].(string); ok && role == "assistant" {
							if id, ok := msg["id"].(string); ok {
								messageID = id
							}
						}
					}
				}
				if convID, ok := v["conversation_id"].(string); ok {
					conversationID = convID
				}
			}
		}
	}

	return conversationID, messageID
}

func extractReplyContent(content string) string {
	var replyContent string
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data:") {
			jsonStr := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			var data Data
			if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
				fmt.Printf("解析JSON失败: %v\n", err)
				continue
			}
			if data.P == "/message/content/parts/0" && data.O == "append" {
				if text, ok := data.V.(string); ok {
					replyContent = text
				}
			}
		}
	}
	return replyContent
}

func clearCookieInConfig() {
	// 清空 Cookie 的操作
	configFile, err := os.Open("config.json")
	if err != nil {
		log.Fatalf("打开配置文件失败: %v", err)
	}
	defer configFile.Close()

	var config Config
	if err := json.NewDecoder(configFile).Decode(&config); err != nil {
		log.Fatalf("解析配置文件失败: %v", err)
	}
	config.Cookie = ""

	updateConfigFile(config)
}
