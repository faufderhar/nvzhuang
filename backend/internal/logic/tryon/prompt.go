// Package tryon 换装提示词模板，针对不同模型优化
package tryon

// BuildGPTSafety 构建 GPT-Image 专用的防封控安全提示词
func BuildGPTSafety(lang string) string {
	if lang == "zh" {
		return gptSafetyZhPrompt
	}
	return gptSafetyEnPrompt
}

// BuildGemini 构建 Gemini（Nano Banana 2）图像模型的提示词（保留原提示词）
func BuildGemini(lang string) string {
	// Gemini 对中英文都支持良好，但换装任务英文指令更稳定
	return enPrompt
}

// BuildDefault 通用/其他模型（如豆包等）提示词（保留原提示词）
func BuildDefault(lang string) string {
	if lang == "zh" {
		return zhPrompt
	}
	return enPrompt
}

// BuildGrok 构建 grok-imagine-image 多图编辑提示词。
// 参考图顺序固定为第 1 张人物、第 2 张服装；必须明确禁止把服装图里的模特一起合成进来。
func BuildGrok(lang string) string {
	if lang == "zh" {
		return grokZhPrompt
	}
	return grokEnPrompt
}

// gptSafetyEnPrompt GPT-Image-2 专用英文防封控安全提示词
const gptSafetyEnPrompt = `Task: Professional commercial fashion virtual try-on photograph.

Strict Requirements:
1. Model Identity: Maintain the adult female model from Image 1 - faithfully preserve her facial features, ethnic identity, eye shape, smile, skin tone, and hairstyle. Keep her identity consistent.
2. Wardrobe: Dress her in the exact clothing and accessories shown in Image 2. Faithfully reproduce the colors, fabric textures, patterns, and silhouette.
3. Safety & Fit: Professional high-end commercial lookbook standard. Fully clothed, modest neckline, standard tailored fit, upright standing posture, non-revealing.
4. Composition: Clean bright studio lighting, neutral minimal background, photorealistic 8k, high detail fashion editorial photograph.

Output: ONLY the final dressed-up photograph.`

// gptSafetyZhPrompt GPT-Image-2 专用中文防封控安全提示词
const gptSafetyZhPrompt = `任务：专业商业时尚虚拟试衣摄影。

严格要求：
1. 模特身份：保持第一张图片中的成年女性模特身份——忠实保留其面部特征、种族相貌、眼部轮廓、肤色与发型，保持人物相貌风格一致。
2. 服装穿搭：让人物完整穿上第二张图片中展示的服装与配饰，忠实还原服装的色彩、面料质感、图案与款式剪裁。
3. 安全与合规：采用高端商业型录摄影标准。穿着完整端庄，标准合身剪裁，端正站姿，避免过度露肤与暴露。
4. 画面效果：纯净明亮的商业影棚光照，简洁中性背景，逼真细节与质感，专业时尚画册品质。

输出：仅输出最终的完整着装照片。`

// enPrompt 英文提示词（主用，效果最稳定）
const enPrompt = `You are a professional fashion virtual try-on assistant.

Task: Dress the person in the first image wearing the garment in the second image, and output a single photorealistic full-body or half-body photo.

Strict requirements:
1. Keep the person's identity EXACTLY the same: same face, same facial features, same skin tone, same hairstyle, same body type and proportions. Do not change who they are.
2. Keep the person's original pose, body angle and orientation as much as possible; only adjust the arms/limbs if strictly needed to fit the garment naturally.
3. Put the exact garment from the second image onto the person: preserve its color, fabric texture, pattern, neckline, sleeve length, hem length and silhouette faithfully. Do not redesign or simplify the garment.
4. The garment must drape naturally on the body, follow the body contours, and cast realistic folds, shadows and highlights consistent with the original lighting.
5. Keep the original background, lighting and overall mood of the person's photo.
6. Photorealistic, high detail, professional fashion editorial quality, 8k, natural skin texture.

Output: ONLY the final dressed-up photograph, nothing else.`

// zhPrompt 中文提示词（备用）
const zhPrompt = `你是一名专业的时尚虚拟试衣助手。

任务：让第一张图片中的人物穿上第二张图片中的服装，输出一张逼真的全身或半身照片。

严格要求：
1. 人物身份必须完全一致：相同的脸、五官、肤色、发型、身材比例。不得改变人物是谁。
2. 尽量保持人物原有的姿势、身体角度和朝向；仅在必要时微调手臂/肢体以自然贴合服装。
3. 将第二张图中的服装原样穿到人物身上：忠实保留其颜色、面料质感、图案、领型、袖长、下摆长度和廓形。不得重新设计或简化服装。
4. 服装必须自然垂坠贴合身体曲线，并根据原始光照产生真实的褶皱、阴影和高光。
5. 保留人物照片原有的背景、光线和整体氛围。
6. 逼真、高细节、专业时尚大片质感、8k、自然皮肤纹理。

输出：仅输出最终的换装照片，不要其他内容。`

// grokEnPrompt grok-imagine-image 多图编辑：第 1 张是人物，第 2 张只提供服装。
const grokEnPrompt = `You are a professional fashion virtual try-on assistant.

The reference images are ordered:
- Image 1 is the person. This is the only person who may appear in the result.
- Image 2 is the clothing reference only. Use its garments, and nothing else from that photo.

Task: Dress the person from Image 1 in the exact outfit from Image 2, and output one photorealistic photo.

Strict requirements:
1. Keep Image 1 exactly: same face, facial features, skin tone, hairstyle, body proportions, pose, camera angle, background, and lighting. Do not replace this person.
2. Reproduce Image 2's outfit exactly: colors, patterns, fabric, neckline, sleeves, hem length, shoes, and accessories. Do not redesign, simplify, or substitute a different outfit.
3. Do not copy the model, face, body, pose, or background from Image 2. Do not show both people. Do not add anyone else.
4. The outfit must follow the body with natural folds, shadows, and highlights consistent with Image 1's lighting.
5. Photorealistic, high detail, natural skin texture.

Output only the final photograph.`

// grokZhPrompt grok-imagine-image 多图编辑中文提示词。
const grokZhPrompt = `你是一名专业的时尚虚拟试衣助手。

参考图顺序如下：
- 第 1 张是人物。结果里只允许出现这个人。
- 第 2 张只作为服装参考。只使用其中的服装，不要使用这张照片里的其他内容。

任务：让第 1 张里的人物穿上第 2 张里的整套服装，输出一张逼真照片。

严格要求：
1. 完整保留第 1 张的人物：相同的脸、五官、肤色、发型、身材比例、姿势、机位、背景和光线。不得换成另一个人。
2. 原样还原第 2 张的服装：颜色、图案、面料、领型、袖长、下摆、鞋子和配饰。不得重新设计、简化或换成另一套衣服。
3. 不得复制第 2 张里的模特、脸、身材、姿势或背景。不得把两个人同时画进画面，也不得添加其他人。
4. 服装必须贴合第 1 张人物的身体，并与其原有光线形成自然的褶皱、阴影和高光。
5. 逼真、高细节、自然皮肤纹理。

只输出最终的换装照片。`
