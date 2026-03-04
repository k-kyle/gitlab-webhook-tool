package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/luoyanke/gitlab-webhook-tool/internal"
	"gopkg.in/yaml.v3"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
	"time"
)

func main() {

	var feishuWebhook string
	var configPath string
	//开启的端口
	var port int

	// 解析命令行参数
	flag.StringVar(&feishuWebhook, "feishuWebhook", "", "")
	flag.StringVar(&configPath, "config", "", "")
	flag.IntVar(&port, "port", 6710, "6710")
	flag.Parse()

	webhookRoutes := map[string]string{}
	if configPath != "" {
		routes, err := loadWebhookRouteConfig(configPath)
		if err != nil {
			log.Printf("load config failed: %v", err)
			os.Exit(1)
		}
		webhookRoutes = routes
	}

	log.Printf("service starting, port=%d, feishuWebhookConfigured=%t, config=%s, routeCount=%d",
		port, feishuWebhook != "", configPath, len(webhookRoutes))
	if feishuWebhook == "" && len(webhookRoutes) == 0 {
		log.Print("warning: no webhook configured, notifications will fail")
	}

	http.HandleFunc("/web-hook", func(writer http.ResponseWriter, request *http.Request) {
		log.Printf("incoming webhook request: method=%s path=%s remote=%s", request.Method, request.URL.Path, request.RemoteAddr)
		if request.Method != http.MethodPost {
			writeJSONResponse(writer, http.StatusMethodNotAllowed, "method not allowed, only POST is supported", nil)
			return
		}
		bodyBytes, err := ioutil.ReadAll(request.Body)
		defer request.Body.Close()
		if err != nil {
			log.Printf("read webhook payload failed: %v", err)
			writeJSONResponse(writer, http.StatusBadRequest, "read request body failed", map[string]interface{}{
				"error": err.Error(),
			})
			return
		}
		log.Printf("webhook payload size=%d bytes", len(bodyBytes))
		//body := string(bodyBytes)
		//log.Print(body)

		var baseBody internal.BaseBody
		err = json.Unmarshal(bodyBytes, &baseBody)
		if err != nil {
			log.Printf("invalid webhook payload: %v", err)
			writeJSONResponse(writer, http.StatusBadRequest, "invalid webhook payload", map[string]interface{}{
				"error": err.Error(),
			})
			return
		}
		if baseBody.ObjectKind == "merge_request" {
			log.Print("dispatch webhook: object_kind=merge_request")
			if err = mergeRequestNotify(bodyBytes, feishuWebhook, webhookRoutes); err != nil {
				log.Printf("merge_request notify failed: %v", err)
				writeJSONResponse(writer, http.StatusInternalServerError, "merge_request notify failed", map[string]interface{}{
					"error": err.Error(),
				})
				return
			}
			writeJSONResponse(writer, http.StatusOK, "merge_request processed", map[string]interface{}{
				"object_kind": "merge_request",
			})
		} else if baseBody.ObjectKind == "push" {
			log.Print("dispatch webhook: object_kind=push")
			if err = pushNotify(bodyBytes, feishuWebhook, webhookRoutes); err != nil {
				log.Printf("push notify failed: %v", err)
				writeJSONResponse(writer, http.StatusInternalServerError, "push notify failed", map[string]interface{}{
					"error": err.Error(),
				})
				return
			}
			writeJSONResponse(writer, http.StatusOK, "push processed", map[string]interface{}{
				"object_kind": "push",
			})
		} else {
			log.Printf("ignore webhook: unsupported object_kind=%s", baseBody.ObjectKind)
			writeJSONResponse(writer, http.StatusOK, "webhook ignored: unsupported object_kind", map[string]interface{}{
				"object_kind": baseBody.ObjectKind,
			})
		}
	})

	// 启动 HTTP 服务器
	log.Printf("webhook server listening on :%d", port)
	if err := http.ListenAndServe(":"+strconv.Itoa(port), nil); err != nil {
		log.Printf("http server stopped: %v", err)
		os.Exit(1)
	}
}

type WebhookRouteConfig struct {
	Routes map[string]string `json:"routes" yaml:"routes"`
}

type APIResponse struct {
	Code    int                    `json:"code"`
	Message string                 `json:"message"`
	Data    map[string]interface{} `json:"data,omitempty"`
}

func writeJSONResponse(writer http.ResponseWriter, status int, message string, data map[string]interface{}) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	resp := APIResponse{
		Code:    status,
		Message: message,
		Data:    data,
	}
	if err := json.NewEncoder(writer).Encode(resp); err != nil {
		log.Printf("write response failed: %v", err)
	}
}

func loadWebhookRouteConfig(configPath string) (map[string]string, error) {
	content, err := ioutil.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("read config file failed: %w", err)
	}

	var cfg WebhookRouteConfig
	ext := strings.ToLower(filepath.Ext(configPath))
	switch ext {
	case ".yaml", ".yml":
		if err = yaml.Unmarshal(content, &cfg); err != nil {
			return nil, fmt.Errorf("parse yaml config failed: %w", err)
		}
	case ".json":
		if err = json.Unmarshal(content, &cfg); err != nil {
			return nil, fmt.Errorf("parse json config failed: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported config extension: %s", ext)
	}

	if len(cfg.Routes) == 0 {
		return nil, fmt.Errorf("config routes is empty")
	}

	routes := map[string]string{}
	for key, value := range cfg.Routes {
		k := strings.TrimSpace(key)
		v := strings.TrimSpace(value)
		if k == "" || v == "" {
			continue
		}
		routes[k] = v
	}
	if len(routes) == 0 {
		return nil, fmt.Errorf("config routes has no valid entries")
	}
	return routes, nil
}

func resolveWebhook(projectName string, projectPathWithNamespace string, defaultWebhook string, routes map[string]string) (string, string, error) {
	lookupKeys := []string{
		strings.TrimSpace(projectPathWithNamespace),
		strings.TrimSpace(projectName),
		"*",
	}

	for _, key := range lookupKeys {
		if key == "" {
			continue
		}
		if webhook, ok := routes[key]; ok && strings.TrimSpace(webhook) != "" {
			return webhook, key, nil
		}
	}
	if strings.TrimSpace(defaultWebhook) != "" {
		return defaultWebhook, "flag:-feishuWebhook", nil
	}
	return "", "", fmt.Errorf("no webhook route found for project=%s namespace=%s", projectName, projectPathWithNamespace)
}

func renderCard(templateName string, templateContent string, data map[string]interface{}) (string, error) {
	var writer bytes.Buffer
	tmpl, err := template.New(templateName).Parse(templateContent)
	if err != nil {
		return "", fmt.Errorf("parse %s template failed: %w", templateName, err)
	}
	if err = tmpl.Execute(&writer, data); err != nil {
		return "", fmt.Errorf("render %s card failed: %w", templateName, err)
	}
	return writer.String(), nil
}

func sendFeishuCard(feishuWebhook string, card string) (*FeishuWebHookResp, error) {
	if feishuWebhook == "" {
		return nil, fmt.Errorf("feishuWebhook is empty")
	}

	cardBody := internal.FeishuCard{
		MsgType: "interactive",
		Card:    card,
	}
	marshal, err := json.Marshal(cardBody)
	if err != nil {
		return nil, fmt.Errorf("marshal card failed: %w", err)
	}

	req, err := http.NewRequest("POST", feishuWebhook, bytes.NewBuffer(marshal))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	bodyBytesResp, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read feishu response failed: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("feishu response status=%s body=%s", resp.Status, string(bodyBytesResp))
	}

	var result FeishuWebHookResp
	if err = json.Unmarshal(bodyBytesResp, &result); err != nil {
		return nil, fmt.Errorf("parse feishu response failed: %w, body=%s", err, string(bodyBytesResp))
	}
	if result.Code != 0 {
		return &result, fmt.Errorf("feishu response code=%d msg=%s", result.Code, result.Msg)
	}

	return &result, nil
}

func mergeRequestNotify(bodyBytes []byte, defaultWebhook string, routes map[string]string) error {
	var body internal.MergeRequestBody
	err := json.Unmarshal(bodyBytes, &body)
	if err != nil {
		log.Printf("parse merge_request payload failed: %v", err)
		return err
	}
	feishuWebhook, routeKey, err := resolveWebhook(body.Project.Name, body.Project.PathWithNamespace, defaultWebhook, routes)
	if err != nil {
		return err
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

	card, err := renderCard("merge_request", internal.MergeRequestFeishuCardTmpl(), map[string]interface{}{
		"projectName":  body.Project.Name,
		"userName":     body.User.Username + "(" + body.User.Name + ")",
		"sourceBranch": body.ObjectAttributes.SourceBranch,
		"targetBranch": body.ObjectAttributes.TargetBranch,
		"webUrl":       body.Project.WebURL,
		"title":        title,
		"headerColor":  headerColor,
	})
	if err != nil {
		return err
	}
	if _, err = sendFeishuCard(feishuWebhook, card); err != nil {
		return err
	}
	log.Printf("merge_request notify success: project=%s state=%s route=%s", body.Project.Name, body.ObjectAttributes.State, routeKey)
	return nil
}

func pushNotify(bodyBytes []byte, defaultWebhook string, routes map[string]string) error {
	var body internal.PushRequestBody

	err := json.Unmarshal(bodyBytes, &body)
	if err != nil {
		log.Printf("parse push payload failed: %v", err)
		return err
	}
	feishuWebhook, routeKey, err := resolveWebhook(body.Project.Name, body.Project.PathWithNamespace, defaultWebhook, routes)
	if err != nil {
		return err
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

	card, err := renderCard("push", internal.PushFeishuCardTmpl(), map[string]interface{}{
		"projectName": body.Project.Name,
		"userName":    body.UserName,
		"ref":         body.Ref,
		"webUrl":      body.Project.WebURL,
		"commit":      commits,
		"title":       title,
		"headerColor": headerColor,
	})
	if err != nil {
		return err
	}
	result, err := sendFeishuCard(feishuWebhook, card)
	if err != nil {
		if result != nil {
			bytearray, _ := json.Marshal(result)
			log.Print(string(bytearray))
		}
		log.Print(card)
		return err
	}
	log.Printf("push notify success: project=%s ref=%s commits=%d route=%s", body.Project.Name, body.Ref, len(body.Commits), routeKey)

	return nil
}

type FeishuWebHookResp struct {
	Code int                    `json:"code"`
	Data map[string]interface{} `json:"data"`
	Msg  string                 `json:"msg"`
}
