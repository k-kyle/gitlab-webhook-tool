package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"github.com/luoyanke/gitlab-webhook-tool/internal"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"text/template"
)

func main() {

	var feishuWebhook string
	//开启的端口
	var port int

	// 解析命令行参数
	flag.StringVar(&feishuWebhook, "feishuWebhook", "", "")
	flag.IntVar(&port, "port", 6710, "6710")
	flag.Parse()
	log.Printf("service starting, port=%d, feishuWebhookConfigured=%t", port, feishuWebhook != "")
	if feishuWebhook == "" {
		log.Print("warning: feishuWebhook is empty, notifications will fail")
	}

	http.HandleFunc("/web-hook", func(writer http.ResponseWriter, request *http.Request) {
		log.Printf("incoming webhook request: method=%s path=%s remote=%s", request.Method, request.URL.Path, request.RemoteAddr)
		var bodyBytes, _ = ioutil.ReadAll(request.Body)
		defer request.Body.Close()
		log.Printf("webhook payload size=%d bytes", len(bodyBytes))
		//body := string(bodyBytes)
		//log.Print(body)

		var baseBody internal.BaseBody
		err := json.Unmarshal(bodyBytes, &baseBody)
		if err != nil {
			log.Printf("invalid webhook payload: %v", err)
			return
		}
		if baseBody.ObjectKind == "merge_request" {
			log.Print("dispatch webhook: object_kind=merge_request")
			mergeRequestNotify(bodyBytes, feishuWebhook)
		} else if baseBody.ObjectKind == "push" {
			log.Print("dispatch webhook: object_kind=push")
			pushNotify(bodyBytes, feishuWebhook)
		} else {
			log.Printf("ignore webhook: unsupported object_kind=%s", baseBody.ObjectKind)
		}
	})

	// 启动 HTTP 服务器
	log.Printf("webhook server listening on :%d", port)
	if err := http.ListenAndServe(":"+strconv.Itoa(port), nil); err != nil {
		log.Printf("http server stopped: %v", err)
		os.Exit(1)
	}
}

func mergeRequestNotify(bodyBytes []byte, feishuWebhook string) {
	var body internal.MergeRequestBody
	var writer bytes.Buffer
	err := json.Unmarshal(bodyBytes, &body)
	if err != nil {
		log.Printf("parse merge_request payload failed: %v", err)
		return
	}
	log.Printf("merge_request event: project=%s state=%s source=%s target=%s user=%s",
		body.Project.Name, body.ObjectAttributes.State, body.ObjectAttributes.SourceBranch, body.ObjectAttributes.TargetBranch, body.User.Username)

	var title string
	var headerColor string
	if body.ObjectAttributes.State == "opened" {
		title = body.Project.Name + " 合并请求提交事件"
		headerColor = "blue"
	} else if body.ObjectAttributes.State == "merged" {
		title = body.Project.Name + " 合并请求完成事件"
		headerColor = "blue"
	} else if body.ObjectAttributes.State == "closed" {
		title = body.Project.Name + " 合并请求关闭事件"
		headerColor = "red"
	} else {
		title = body.Project.Name + " 合并请求事件"
		headerColor = "blue"
	}

	tmpl, _ := template.New("index").Parse(internal.MergeRequestFeishuCardTmpl())
	tmpl.Execute(&writer, map[string]interface{}{
		"projectName":  body.Project.Name,
		"userName":     body.User.Username + "(" + body.User.Name + ")",
		"sourceBranch": body.ObjectAttributes.SourceBranch,
		"targetBranch": body.ObjectAttributes.TargetBranch,
		"webUrl":       body.Project.WebURL,
		"title":        title,
		"headerColor":  headerColor,
	})
	var cardBody internal.FeishuCard
	cardBody.MsgType = "interactive"
	cardBody.Card = writer.String()
	//log.Print(cardBody.Card)
	marshal, err := json.Marshal(cardBody)
	req, err := http.NewRequest("POST", feishuWebhook, bytes.NewBuffer(marshal))
	if err != nil {
		log.Fatalln(err)
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Fatalln(err)
	} else {
		log.Printf("merge_request notify sent, feishuStatus=%s", resp.Status)
		//var bodyBytes, _ = ioutil.ReadAll(req.Body)
		//s := string(bodyBytes)
		//log.Print(s)
	}
	defer resp.Body.Close()
}

func pushNotify(bodyBytes []byte, feishuWebhook string) {
	var body internal.PushRequestBody
	var writer bytes.Buffer

	err := json.Unmarshal(bodyBytes, &body)
	if err != nil {
		log.Printf("parse push payload failed: %v", err)
		return
	}
	log.Printf("push event: project=%s ref=%s user=%s commits=%d", body.Project.Name, body.Ref, body.UserName, len(body.Commits))
	var commits string
	for index, obj := range body.Commits {
		msg := strings.ReplaceAll(obj.Message, "\n", "")
		msg = strings.ReplaceAll(msg, "	", " ")
		if len(msg) > 600 {
			msg = msg[0:600] + "..."
		}
		modifiedMsg := "- 变更文件：\\n"
		if len(obj.Modified) > 0 {
			showCount := len(obj.Modified)
			if showCount > 10 {
				showCount = 10
			}
			for i := 0; i < showCount; i++ {
				modifiedMsg += "  - `" + obj.Modified[i] + "`\\n"
			}
			if len(obj.Modified) > showCount {
				modifiedMsg += "  - ... 其余 " + strconv.Itoa(len(obj.Modified)-showCount) + " 个文件\\n"
			}
		} else {
			modifiedMsg += "  - （无）\\n"
		}
		commits += "**Commit " + strconv.Itoa(index+1) + "**\\n" +
			"- 提交人：**" + obj.Author.Name + "** <" + obj.Author.Email + ">\\n" +
			"- 摘要：" + msg + "\\n" +
			modifiedMsg +
			"- 🔗 [查看 Commit](" + obj.URL + ")\\n\\n"
		if index > 8 {
			i := len(body.Commits) - index
			commits += "_... 其余 " + strconv.Itoa(i) + " 条 commit 省略_\\n"
			break
		}
	}
	var title string
	var headerColor string
	if body.After == "0000000000000000000000000000000000000000" {
		title = body.Project.Name + " 删除代码分支事件"
		headerColor = "red"
	} else {
		title = body.Project.Name + " 代码推送事件"
		headerColor = "turquoise"
	}

	tmpl, _ := template.New("index").Parse(internal.PushFeishuCardTmpl())
	tmpl.Execute(&writer, map[string]interface{}{
		"projectName": body.Project.Name,
		"userName":    body.UserName,
		"ref":         body.Ref,
		"webUrl":      body.Project.WebURL,
		"commit":      commits,
		"title":       title,
		"headerColor": headerColor,
	})

	var cardBody internal.FeishuCard
	cardBody.MsgType = "interactive"
	cardBody.Card = writer.String()
	//log.Print(cardBody.Card)
	marshal, err := json.Marshal(cardBody)
	req, err := http.NewRequest("POST", feishuWebhook, bytes.NewBuffer(marshal))
	if err != nil {
		log.Fatalln(err)
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Fatalln(err)
	} else {
		defer resp.Body.Close()
		var result FeishuWebHookResp
		var bodyBytes, _ = ioutil.ReadAll(resp.Body)
		err := json.Unmarshal(bodyBytes, &result)
		if err != nil {
			log.Print(err)
			return
		}
		if result.Code != 0 {
			bytearray, _ := json.Marshal(result)
			log.Print(string(bytearray))
			log.Print(cardBody)
		} else {
			log.Printf("push notify success: project=%s ref=%s commits=%d", body.Project.Name, body.Ref, len(body.Commits))
		}
		//var bodyBytes, _ = ioutil.ReadAll(resp.Body)
		//log.Print(string(bodyBytes))
		//var bodyBytes, _ = ioutil.ReadAll(req.Body)
		//s := string(bodyBytes)
		//log.Print(s)
	}

}

type FeishuWebHookResp struct {
	Code int                    `json:"code"`
	Data map[string]interface{} `json:"data"`
	Msg  string                 `json:"msg"`
}
