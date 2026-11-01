package tryon

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ComfyUI 使用服务端固定的 API 工作流调用本机 ComfyUI。
// Base URL、工作流路径和节点 ID 只能来自后端配置，不能由浏览器请求覆盖。
type ComfyUI struct {
	name           string
	baseURL        string
	model          string
	workflowPath   string
	personNode     string
	garmentNode    string
	promptNode     string
	seedNode       string
	seed           int64
	outputNode     string
	pollInterval   time.Duration
	requestTimeout time.Duration
	client         *http.Client
}

type comfyUploadResponse struct {
	Name      string `json:"name"`
	Subfolder string `json:"subfolder"`
	Type      string `json:"type"`
}

type comfyPromptRequest struct {
	Prompt   map[string]interface{} `json:"prompt"`
	ClientID string                 `json:"client_id"`
}

type comfyPromptResponse struct {
	PromptID   string                 `json:"prompt_id"`
	NodeErrors map[string]interface{} `json:"node_errors"`
}

type comfyImageRef struct {
	Filename  string `json:"filename"`
	Subfolder string `json:"subfolder"`
	Type      string `json:"type"`
}

type comfyHistoryOutput struct {
	Images []comfyImageRef `json:"images"`
}

type comfyHistoryEntry struct {
	Outputs map[string]comfyHistoryOutput `json:"outputs"`
	Status  struct {
		Completed bool   `json:"completed"`
		StatusStr string `json:"status_str"`
	} `json:"status"`
}

func NewComfyUI(cfg *ModelConfig) *ComfyUI {
	pollInterval := cfg.PollInterval
	if pollInterval <= 0 {
		pollInterval = time.Second
	}
	requestTimeout := cfg.RequestTimeout
	if requestTimeout <= 0 {
		requestTimeout = 15 * time.Minute
	}
	return &ComfyUI{
		name:           cfg.Name,
		baseURL:        strings.TrimRight(cfg.BaseURL, "/"),
		model:          cfg.Model,
		workflowPath:   cfg.WorkflowPath,
		personNode:     cfg.PersonNode,
		garmentNode:    cfg.GarmentNode,
		promptNode:     cfg.PromptNode,
		seedNode:       cfg.SeedNode,
		seed:           cfg.Seed,
		outputNode:     cfg.OutputNode,
		pollInterval:   pollInterval,
		requestTimeout: requestTimeout,
		client:         newModelHTTPClient(false),
	}
}

func (c *ComfyUI) Name() string {
	if c.name == "" {
		return "comfyui"
	}
	return c.name
}

// comfyHealthTTL 健康检查成功结果的缓存时长，避免每次换装/健康探测都重复拉取 /object_info。
var comfyHealthTTL = 30 * time.Second

var comfyHealthCache sync.Map // key -> time.Time（最近一次成功时间）

func (c *ComfyUI) healthCacheKey() string {
	return strings.Join([]string{c.baseURL, c.workflowPath, c.personNode, c.garmentNode, c.promptNode, c.outputNode}, "|")
}

// Health 验证 ComfyUI API、固定工作流和工作流所需节点是否可用，不执行推理。
// 成功结果缓存 comfyHealthTTL，失败不缓存。
func (c *ComfyUI) Health(ctx context.Context) error {
	key := c.healthCacheKey()
	if v, ok := comfyHealthCache.Load(key); ok && comfyHealthTTL > 0 && time.Since(v.(time.Time)) < comfyHealthTTL {
		return nil
	}
	if err := c.health(ctx); err != nil {
		comfyHealthCache.Delete(key)
		return err
	}
	comfyHealthCache.Store(key, time.Now())
	return nil
}

func (c *ComfyUI) health(ctx context.Context) error {
	if c.baseURL == "" {
		return fmt.Errorf("ComfyUI 地址未配置")
	}
	workflow, err := c.loadWorkflow()
	if err != nil {
		return err
	}
	if err := c.validateWorkflow(workflow); err != nil {
		return err
	}
	if _, err := c.getJSON(ctx, "/system_stats"); err != nil {
		return fmt.Errorf("无法连接 ComfyUI")
	}
	objectInfo, err := c.getJSON(ctx, "/object_info")
	if err != nil {
		return fmt.Errorf("无法读取 ComfyUI 节点信息")
	}
	var available map[string]json.RawMessage
	if err := json.Unmarshal(objectInfo, &available); err != nil {
		return fmt.Errorf("ComfyUI 节点信息格式错误")
	}
	for _, node := range workflow {
		item, ok := node.(map[string]interface{})
		if !ok {
			continue
		}
		classType, _ := item["class_type"].(string)
		if classType != "" {
			rawNodeInfo, ok := available[classType]
			if !ok {
				return fmt.Errorf("ComfyUI 缺少固定工作流节点: %s", classType)
			}
			// 校验模型输入文件是否存在于 ComfyUI 可用模型列表中
			if inputs, ok := item["inputs"].(map[string]interface{}); ok {
				var nodeInfo struct {
					Input struct {
						Required map[string]interface{} `json:"required"`
					} `json:"input"`
				}
				if err := json.Unmarshal(rawNodeInfo, &nodeInfo); err == nil && nodeInfo.Input.Required != nil {
					for paramName, paramVal := range inputs {
						paramStr, isStr := paramVal.(string)
						if !isStr || strings.HasPrefix(paramStr, "__") {
							continue
						}
						if fieldMeta, ok := nodeInfo.Input.Required[paramName]; ok {
							if fieldArr, ok := fieldMeta.([]interface{}); ok && len(fieldArr) > 0 {
								if options, ok := fieldArr[0].([]interface{}); ok && len(options) > 0 {
									found := false
									for _, opt := range options {
										if optStr, ok := opt.(string); ok && optStr == paramStr {
											found = true
											break
										}
									}
									if !found {
										return fmt.Errorf("ComfyUI 缺少模型文件: %s", paramStr)
									}
								}
							}
						}
					}
				}
			}
		}
	}
	return nil
}

func (c *ComfyUI) Generate(ctx context.Context, personB64, garmentB64, prompt, size string) (string, error) {
	if err := c.Health(ctx); err != nil {
		return "", err
	}

	personData, err := base64.StdEncoding.DecodeString(ensureBase64(personB64))
	if err != nil {
		return "", fmt.Errorf("人物图片格式错误")
	}
	garmentData, err := base64.StdEncoding.DecodeString(ensureBase64(garmentB64))
	if err != nil {
		return "", fmt.Errorf("服装图片格式错误")
	}

	runID := randomComfyID()
	personName, err := c.uploadImage(ctx, "person-"+runID, personData)
	if err != nil {
		return "", err
	}
	garmentName, err := c.uploadImage(ctx, "garment-"+runID, garmentData)
	if err != nil {
		return "", err
	}

	workflow, err := c.loadWorkflow()
	if err != nil {
		return "", err
	}
	if err := setComfyInput(workflow, c.personNode, "image", personName); err != nil {
		return "", err
	}
	if err := setComfyInput(workflow, c.garmentNode, "image", garmentName); err != nil {
		return "", err
	}
	if err := setComfyInput(workflow, c.promptNode, "prompt", c.buildPrompt(prompt)); err != nil {
		return "", err
	}
	if c.seedNode != "" {
		seed := c.seed
		if seed < 0 {
			seed = time.Now().UnixNano() & 0x7fffffffffffffff
		}
		if err := setComfySeed(workflow, c.seedNode, seed); err != nil {
			return "", err
		}
	}
	// PreviewImage 输出写入 ComfyUI temp 目录（重启即清空），无 filename_prefix 输入；
	// SaveImage 会把结果永久写入 output 目录，需要运维定期清理（见 DEPLOY.md）。
	if comfyNodeClass(workflow, c.outputNode) != "PreviewImage" {
		if err := setComfyInput(workflow, c.outputNode, "filename_prefix", "tryon/local-"+runID); err != nil {
			return "", err
		}
	}

	promptID, err := c.queuePrompt(ctx, workflow, runID)
	if err != nil {
		return "", err
	}

	waitCtx, cancel := context.WithTimeout(ctx, c.requestTimeout)
	defer cancel()
	imageRef, err := c.waitForImage(waitCtx, promptID)
	if err != nil {
		// 客户端断开或超时：取消排队/中断执行，避免继续占用 GPU
		c.cancelPrompt(promptID)
		return "", err
	}
	imageData, err := c.downloadImage(waitCtx, imageRef)
	c.deleteHistory(promptID)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(imageData), nil
}

func comfyNodeClass(workflow map[string]interface{}, nodeID string) string {
	node, _ := workflow[nodeID].(map[string]interface{})
	classType, _ := node["class_type"].(string)
	return classType
}

// comfyCleanupTimeout 清理请求使用独立的短超时上下文，不受已取消的请求上下文影响。
const comfyCleanupTimeout = 5 * time.Second

func (c *ComfyUI) postJSON(path string, payload interface{}) bool {
	body, err := json.Marshal(payload)
	if err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), comfyCleanupTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return false
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

// cancelPrompt 从队列删除任务；若任务正在执行，则只中断该 prompt_id（不影响其他任务）。
func (c *ComfyUI) cancelPrompt(promptID string) {
	if promptID == "" {
		return
	}
	_ = c.postJSON("/queue", map[string]interface{}{"delete": []string{promptID}})
	if c.isPromptRunning(promptID) {
		_ = c.postJSON("/interrupt", map[string]string{"prompt_id": promptID})
	}
	c.deleteHistory(promptID)
}

func (c *ComfyUI) isPromptRunning(promptID string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), comfyCleanupTimeout)
	defer cancel()
	body, err := c.getJSON(ctx, "/queue")
	if err != nil {
		return false
	}
	var queue struct {
		Running [][]interface{} `json:"queue_running"`
	}
	if err := json.Unmarshal(body, &queue); err != nil {
		return false
	}
	for _, item := range queue.Running {
		if len(item) > 1 {
			if id, _ := item[1].(string); id == promptID {
				return true
			}
		}
	}
	return false
}

// deleteHistory 删除任务历史，避免他人通过 ComfyUI /history 枚举到本次换装的输入输出文件名。
func (c *ComfyUI) deleteHistory(promptID string) {
	if promptID == "" {
		return
	}
	_ = c.postJSON("/history", map[string]interface{}{"delete": []string{promptID}})
}

func (c *ComfyUI) uploadImage(ctx context.Context, prefix string, data []byte) (string, error) {
	ext := imageExtension(data)
	filename := prefix + ext
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("image", filename)
	if err != nil {
		return "", fmt.Errorf("无法准备 ComfyUI 图片上传")
	}
	if _, err := part.Write(data); err != nil {
		return "", fmt.Errorf("无法准备 ComfyUI 图片上传")
	}
	// 上传到 temp 目录（ComfyUI 重启时自动清空），文件名带随机 runID 不会互相覆盖；
	// 工作流中以 "<subfolder>/<name> [temp]" 注解引用。
	_ = writer.WriteField("type", "temp")
	_ = writer.WriteField("subfolder", "tryon")
	_ = writer.WriteField("overwrite", "false")
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("无法准备 ComfyUI 图片上传")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/upload/image", &body)
	if err != nil {
		return "", fmt.Errorf("无法创建 ComfyUI 上传请求")
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("无法连接 ComfyUI")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("ComfyUI 图片上传失败")
	}
	var result comfyUploadResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil || result.Name == "" {
		return "", fmt.Errorf("ComfyUI 图片上传响应无效")
	}
	return comfyAnnotatedName(result), nil
}

// comfyAnnotatedName 生成 LoadImage 可识别的带目录注解的文件名，例如 "tryon/person-x.png [temp]"。
func comfyAnnotatedName(r comfyUploadResponse) string {
	name := r.Name
	if r.Subfolder != "" {
		name = r.Subfolder + "/" + name
	}
	switch r.Type {
	case "temp", "output":
		return name + " [" + r.Type + "]"
	default:
		return name
	}
}

func (c *ComfyUI) queuePrompt(ctx context.Context, workflow map[string]interface{}, clientID string) (string, error) {
	body, err := json.Marshal(comfyPromptRequest{Prompt: workflow, ClientID: clientID})
	if err != nil {
		return "", fmt.Errorf("ComfyUI 工作流编码失败")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/prompt", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("无法创建 ComfyUI 任务")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("无法连接 ComfyUI")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("ComfyUI 拒绝了固定工作流")
	}
	var result comfyPromptResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&result); err != nil {
		return "", fmt.Errorf("ComfyUI 任务响应无效")
	}
	if result.PromptID == "" || len(result.NodeErrors) > 0 {
		return "", fmt.Errorf("ComfyUI 固定工作流校验失败")
	}
	return result.PromptID, nil
}

func (c *ComfyUI) waitForImage(ctx context.Context, promptID string) (comfyImageRef, error) {
	ticker := time.NewTicker(c.pollInterval)
	defer ticker.Stop()
	for {
		image, done, err := c.historyImage(ctx, promptID)
		if err != nil {
			return comfyImageRef{}, err
		}
		if done {
			return image, nil
		}
		select {
		case <-ctx.Done():
			return comfyImageRef{}, fmt.Errorf("ComfyUI 生成超时")
		case <-ticker.C:
		}
	}
}

func (c *ComfyUI) historyImage(ctx context.Context, promptID string) (comfyImageRef, bool, error) {
	body, err := c.getJSON(ctx, "/history/"+url.PathEscape(promptID))
	if err != nil {
		return comfyImageRef{}, false, fmt.Errorf("无法读取 ComfyUI 任务状态")
	}
	var history map[string]comfyHistoryEntry
	if err := json.Unmarshal(body, &history); err != nil {
		return comfyImageRef{}, false, fmt.Errorf("ComfyUI 任务状态格式错误")
	}
	entry, ok := history[promptID]
	if !ok {
		return comfyImageRef{}, false, nil
	}
	if output, ok := entry.Outputs[c.outputNode]; ok && len(output.Images) > 0 {
		return output.Images[0], true, nil
	}
	if entry.Status.Completed || strings.EqualFold(entry.Status.StatusStr, "error") {
		return comfyImageRef{}, false, fmt.Errorf("ComfyUI 未生成结果图片")
	}
	return comfyImageRef{}, false, nil
}

func (c *ComfyUI) downloadImage(ctx context.Context, image comfyImageRef) ([]byte, error) {
	values := url.Values{}
	values.Set("filename", image.Filename)
	values.Set("subfolder", image.Subfolder)
	if image.Type == "" {
		image.Type = "output"
	}
	values.Set("type", image.Type)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/view?"+values.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("无法创建 ComfyUI 结果请求")
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("无法读取 ComfyUI 结果图片")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("无法读取 ComfyUI 结果图片")
	}
	data, err := readLimited(resp.Body, modelMaxResponseBytes)
	if err != nil || len(data) == 0 {
		return nil, fmt.Errorf("ComfyUI 结果图片无效")
	}
	return data, nil
}

func (c *ComfyUI) getJSON(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status")
	}
	return readLimited(resp.Body, modelMaxResponseBytes)
}

func (c *ComfyUI) loadWorkflow() (map[string]interface{}, error) {
	path := strings.TrimSpace(c.workflowPath)
	if path == "" {
		return nil, fmt.Errorf("ComfyUI 固定工作流未配置")
	}
	candidates := []string{path}
	if !filepath.IsAbs(path) {
		candidates = append(candidates,
			filepath.Join("backend", path),
			filepath.Join("..", path),
			filepath.Join("..", "..", path),
			filepath.Join("..", "..", "..", path),
		)
	}
	var data []byte
	var err error
	for _, candidate := range candidates {
		data, err = os.ReadFile(candidate)
		if err == nil {
			break
		}
	}
	if err != nil {
		return nil, fmt.Errorf("ComfyUI 固定工作流读取失败")
	}
	var workflow map[string]interface{}
	if err := json.Unmarshal(data, &workflow); err != nil {
		return nil, fmt.Errorf("ComfyUI 固定工作流格式错误")
	}
	return workflow, nil
}

func (c *ComfyUI) validateWorkflow(workflow map[string]interface{}) error {
	for _, nodeID := range []string{c.personNode, c.garmentNode, c.promptNode, c.outputNode} {
		if nodeID == "" {
			return fmt.Errorf("ComfyUI 固定工作流节点未配置")
		}
		if _, ok := workflow[nodeID]; !ok {
			return fmt.Errorf("ComfyUI 固定工作流节点不存在")
		}
	}
	return nil
}

func setComfyInput(workflow map[string]interface{}, nodeID, field string, value interface{}) error {
	rawNode, ok := workflow[nodeID]
	if !ok {
		return fmt.Errorf("ComfyUI 固定工作流节点不存在")
	}
	node, ok := rawNode.(map[string]interface{})
	if !ok {
		return fmt.Errorf("ComfyUI 固定工作流节点格式错误")
	}
	inputs, ok := node["inputs"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("ComfyUI 固定工作流输入格式错误")
	}
	inputs[field] = value
	return nil
}

func setComfySeed(workflow map[string]interface{}, nodeID string, seed int64) error {
	rawNode, ok := workflow[nodeID]
	if !ok {
		return fmt.Errorf("ComfyUI 固定工作流节点不存在")
	}
	node, ok := rawNode.(map[string]interface{})
	if !ok {
		return fmt.Errorf("ComfyUI 固定工作流节点格式错误")
	}
	inputs, ok := node["inputs"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("ComfyUI 固定工作流输入格式错误")
	}
	if _, ok := inputs["seed"]; ok {
		inputs["seed"] = seed
		return nil
	}
	inputs["noise_seed"] = seed
	return nil
}

func (c *ComfyUI) buildPrompt(prompt string) string {
	if strings.Contains(strings.ToLower(c.model), "qwen") || strings.Contains(strings.ToLower(c.workflowPath), "qwen") {
		return buildQwenTryonPrompt(prompt)
	}
	return buildComfyTryonPrompt(prompt)
}

func buildQwenTryonPrompt(extraPrompt string) string {
	base := `Picture 1 is a photo of a person, and Picture 2 shows a garment. Replace the outfit worn by the person in Picture 1 with the exact garment from Picture 2. Keep the person's face, complete identity, hairstyle, facial expression, skin tone, body shape, posture, and the background from Picture 1 completely unchanged. Dress the person naturally in the garment from Picture 2, preserving the outfit's exact colors, patterns, neckline, sleeves, fabric texture, and structural details with realistic fit and lighting.`
	extraPrompt = strings.TrimSpace(extraPrompt)
	if extraPrompt != "" {
		return base + " " + extraPrompt
	}
	return base
}

func buildComfyTryonPrompt(_ string) string {
	// H3 Ref2VA 对固定分段格式的服从度高于通用图片模型提示词。这里不拼接
	// Gemini/GPT 的长提示词，避免重复的“第一张/第二张”描述稀释引用角色。
	return `subject_definitions: Subject A is the adult person whose identity, complete head, face and hair come only from Picture 1. Outfit B, the full-body pose, body placement and catalog composition come from Picture 2.
summary: Replace the empty mannequin or display body in Picture 2 with Subject A and create one centered full-body fashion-catalog portrait of Subject A wearing Outfit B.
retention_analysis: From Picture 1 retain only Subject A's exact identity, full face, forehead, hairline, complete hairstyle, skin tone and natural facial proportions. Do not retain Picture 1's original clothing, close-up crop, selfie camera distance, pose, room or background. From Picture 2 fully retain the centered full-body framing, standing pose, body silhouette, outfit colors, pattern, fabric texture, neckline, sleeve length, hem length, shoes, jewelry and accessories. Replace the empty mannequin, display body and missing head in Picture 2 with Subject A; do not overlay a small person or preserve an empty neck.
detailed_description: [Shot 1] At 0.00 seconds, show one normally proportioned adult person standing centered in a clean full-body catalog portrait, matching Picture 2's scale and pose. Subject A's entire head and all hair are naturally attached to the body and fully inside the frame, with clear empty margin above the hair. The face remains unobstructed, both eyes visible, and identity matches Picture 1. The full body, garment hem, both legs and both shoes remain inside the frame. Subject A wears Outfit B exactly as displayed in Picture 2, with realistic fit, folds, shadows and fabric structure. Use a simple neutral studio background and a fixed camera with no zoom, no push-in, no reframing, no duplicate body, no miniature person and no picture-in-picture composition.
overall_soundscape: N/A
non_diegetic_music: N/A`
}

func imageExtension(data []byte) string {
	switch http.DetectContentType(data) {
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	default:
		return ".png"
	}
}

func randomComfyID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err == nil {
		return hex.EncodeToString(buf)
	}
	return fmt.Sprintf("%x", time.Now().UnixNano())
}

// resolveComfyUIConfig 解析 ComfyUI 模型配置。
// 当 alias 为 "comfyui" 时，优先检测并选用 Qwen 模型（comfyui / comfyui-qwen）；
// 若 Qwen 不可用但配置了 H3（comfyui-h3），则优雅降级到 H3。
// 当 alias 为具体别名（如 comfyui-qwen 或 comfyui-h3）时，直接加载指定配置。
func resolveComfyUIConfig(ctx context.Context, alias string) (*ModelConfig, error) {
	aliasLower := strings.ToLower(strings.TrimSpace(alias))
	if aliasLower != "comfyui" && aliasLower != "" {
		return loadModelConfig(ctx, alias)
	}

	// 1. 尝试首选配置（默认为 comfyui 或 comfyui-qwen）
	primaryCfg, primaryErr := loadModelConfig(ctx, "comfyui")
	if primaryErr != nil {
		primaryCfg, primaryErr = loadModelConfig(ctx, "comfyui-qwen")
	}

	if primaryErr == nil {
		gen := NewComfyUI(primaryCfg)
		checkCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		healthErr := gen.Health(checkCtx)
		cancel()
		if healthErr == nil {
			return primaryCfg, nil
		}
	}

	// 2. 若首选不可用，尝试 fallback 到 comfyui-h3
	h3Cfg, h3Err := loadModelConfig(ctx, "comfyui-h3")
	if h3Err == nil {
		gen := NewComfyUI(h3Cfg)
		checkCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		healthErr := gen.Health(checkCtx)
		cancel()
		if healthErr == nil {
			return h3Cfg, nil
		}
	}

	if primaryErr == nil {
		return primaryCfg, nil
	}
	return nil, primaryErr
}

// CheckComfyUI 为设置中心提供不执行推理的连接检查。
func CheckComfyUI(ctx context.Context) (string, error) {
	cfg, err := resolveComfyUIConfig(ctx, "comfyui")
	if err != nil {
		return "", err
	}
	gen := NewComfyUI(cfg)
	healthCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := gen.Health(healthCtx); err != nil {
		return "", err
	}
	return cfg.Model, nil
}
