package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ClareAI/astra-voice-service/internal/domain"
	"github.com/ClareAI/astra-voice-service/pkg/data/mapping"
	"github.com/ClareAI/astra-voice-service/pkg/logger"
	"go.uber.org/zap"
)

// FetchBrandkit fetches brandkit data from external API
// This is a shared function that can be used by any handler
func FetchBrandkit(agentID string) (*BrandkitResponse, error) {
	brandkitBaseURL := os.Getenv("BRANDKIT_BASE_URL")
	if brandkitBaseURL == "" {
		brandkitBaseURL = "https://dev-astra.engagechat.ai"
	}

	url := fmt.Sprintf("%s/api/mapping/v2/agents/%s/brandkit", brandkitBaseURL, agentID)
	logger.Base().Info("Fetching brandkit from", zap.String("url", url))

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch brandkit: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("brandkit API error (status %d): %s", resp.StatusCode, string(body))
	}

	var brandkit BrandkitResponse
	if err := json.NewDecoder(resp.Body).Decode(&brandkit); err != nil {
		return nil, fmt.Errorf("failed to decode brandkit response: %w", err)
	}

	return &brandkit, nil
}

// GenerateConfigFromBrandkitData generates agent config using AI based on brandkit data
// This is a shared function that can be used by any handler
func GenerateConfigFromBrandkitData(apiKey string, brandkit *BrandkitResponse) (*GeneratedAgentConfig, error) {
	logger.Base().Debug("Generating config from brandkit data")
	var industries []string
	for _, ind := range brandkit.Company.Industries {
		industries = append(industries, ind.Name)
	}

	primaryColor := "#000000"
	for _, color := range brandkit.Colors {
		if color.Type == "dark" || color.Type == "accent" {
			primaryColor = color.Hex
			break
		}
	}

	description := fmt.Sprintf(`Company: %s
Domain: %s
Description: %s

Long Description: %s

Industries: %s
Location: %s, %s

Agent Role: %s

Welcome Message: %s

Conversational Starters:
%s

Brand Colors: %s`,
		brandkit.Name,
		brandkit.Domain,
		brandkit.Description,
		brandkit.LongDescription,
		strings.Join(industries, ", "),
		brandkit.Company.Location.City,
		brandkit.Company.Location.Country,
		brandkit.AgentBrandkitConfig.AgentRoleDescription,
		brandkit.AgentBrandkitConfig.WelcomeMessage,
		strings.Join(brandkit.AgentBrandkitConfig.ConversationalStarters, "\n- "),
		primaryColor,
	)

	systemPrompt := `You are an AI assistant that generates complete voice agent configurations based on company brandkit data.
Based on the provided brandkit information, generate a comprehensive agent configuration in JSON format that includes:
1. persona - The agent's character and role based on the agent_role_description (string)
2. tone - Communication style that matches the brand personality (string: friendly/professional/casual/empathetic/warm)
3. language - Primary language code (string: en/zh/es/fr/ja/de/pt/it/ru/ar)
4. default_accent - Default accent for the primary language (string: optional, e.g. "india", "uk", "us" for en; "mainland", "taiwan" for zh)
5. voice - OpenAI voice name that best fits the brand (string: alloy/ash/ballad/coral/echo/sage/shimmer/verse/marin/cedar)
6. speed - Speech speed (number: 0.5-2.0, default 1.0)
7. services - List of services from company description (array of strings)
8. expertise - Areas of expertise from industries and description (array of strings)
9. greeting_template - Use the welcome_message as base, make it voice-friendly (string)
10. realtime_template - Comprehensive system prompt based on agent_role_description and company info (string)
11. system_instructions - Core behavioral rules aligned with brand values (string)

IMPORTANT: 
- Return ONLY a valid JSON object with these exact field names. No markdown, no explanations.
- Use the provided welcome_message and agent_role_description as primary guidance
- Ensure the greeting and prompts are natural for voice conversation
- Match the brand's personality and values

Example format:
{
  "persona": "Professional customer service representative",
  "tone": "friendly",
  "language": "en",
  "voice": "coral",
  "speed": 1.0,
  "services": ["Product consultation", "Technical support"],
  "expertise": ["Sales", "Customer service"],
  "greeting_template": "Hello! I'm Sarah...",
  "realtime_template": "You are a professional...",
  "system_instructions": "- Always be polite..."
}`

	userPrompt := fmt.Sprintf("Generate a complete voice agent configuration based on this brandkit data:\n\n%s", description)

	responseText, err := CallOpenAI(apiKey, systemPrompt, userPrompt)
	if err != nil {
		return nil, err
	}

	cleanedJSON := cleanMarkdownJSON(responseText)

	var config GeneratedAgentConfig
	if err := json.Unmarshal([]byte(cleanedJSON), &config); err != nil {
		logger.Base().Error("Failed to parse JSON, raw response", zap.String("responsetext", responseText))
		logger.Base().Warn("Cleaned JSON", zap.String("cleanedjson", cleanedJSON))
		return nil, fmt.Errorf("failed to parse agent config JSON: %w", err)
	}

	return &config, nil
}

// CallOpenAI calls OpenAI API
// This is a shared function that can be used by any handler
func CallOpenAI(apiKey, systemPrompt, userPrompt string) (string, error) {
	url := "https://api.openai.com/v1/chat/completions"

	requestBody := map[string]interface{}{
		"model": "gpt-4o",
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userPrompt},
		},
		"temperature": 0.7,
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to call OpenAI API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("OpenAI API error (status %d): %s", resp.StatusCode, string(body))
	}

	var response struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if len(response.Choices) == 0 {
		return "", fmt.Errorf("no response from OpenAI")
	}

	return response.Choices[0].Message.Content, nil
}

// cleanMarkdownJSON removes markdown code block markers from JSON
func cleanMarkdownJSON(text string) string {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	return strings.TrimSpace(text)
}

// GenerateConfigFromBrandkitDataWithTemplate generates agent config using AI based on brandkit data and template voice instructions
func GenerateConfigFromBrandkitDataWithTemplate(apiKey string, brandkit *BrandkitResponse, template *mapping.TemplateV2) (*domain.AgentConfigData, error) {
	logger.Base().Debug("Generating config from brandkit data with template")
	var industries []string
	for _, ind := range brandkit.Company.Industries {
		industries = append(industries, ind.Name)
	}

	primaryColor := "#000000"
	for _, color := range brandkit.Colors {
		if color.Type == "dark" || color.Type == "accent" {
			primaryColor = color.Hex
			break
		}
	}

	description := fmt.Sprintf(`Company: %s
Domain: %s
Description: %s

Long Description: %s

Industries: %s
Location: %s, %s

Agent Role: %s

Welcome Message: %s

Conversational Starters:
%s

Brand Colors: %s`,
		brandkit.Name,
		brandkit.Domain,
		brandkit.Description,
		brandkit.LongDescription,
		strings.Join(industries, ", "),
		brandkit.Company.Location.City,
		brandkit.Company.Location.Country,
		brandkit.AgentBrandkitConfig.AgentRoleDescription,
		brandkit.AgentBrandkitConfig.WelcomeMessage,
		strings.Join(brandkit.AgentBrandkitConfig.ConversationalStarters, "\n- "),
		primaryColor,
	)

	systemPrompt := `You are an AI assistant that generates complete voice agent configurations based on company brandkit data and specific voice instructions.
Based on the provided brandkit information AND the Voice Instructions, generate a comprehensive agent configuration in JSON format.

The Output JSON must strictly follow this structure (matching domain.AgentConfigData):
{
  "persona": "string",
  "tone": "string",
  "language": "string",
  "default_accent": "string",
  "voice": "string",
  "speed": number,
  "services": ["string"],
  "expertise": ["string"],
  "prompt_config": {
    "greeting_template": "string",
    "realtime_template": "string",
    "system_instructions": "string"
  },
  "outbound_prompt_config": {
    "greeting_template": "string",
    "realtime_template": "string",
    "system_instructions": "string"
  }
}

Field Descriptions:
1. persona: The agent's character and role - MUST be based on the brandkit's agent_role_description
2. tone: Communication style (e.g., professional, friendly, empathetic) - match brand personality
3. language: Primary language code (e.g., "en")
4. default_accent: Default accent based on company location (e.g., "india" for India, "us" for USA)
5. voice: OpenAI voice name (e.g., alloy, coral, echo)
6. speed: Speech speed (0.5-2.0)
7. services: Extract specific services/products from brandkit description and longDescription (format example: "Product Name program", "Service Name feature" - use actual services from brandkit)
8. expertise: Extract from industries and company description (format example: "Industry expertise", "Service area" - use actual expertise from brandkit)
9. prompt_config.greeting_template: Use the brandkit's welcome_message, adapt for voice conversation
10. prompt_config.realtime_template: The main system prompt/instructions. 
    CRITICAL REQUIREMENTS:
    - Keep the EXACT FORMAT and STRUCTURE of the provided Voice Instructions template
    - REPLACE all generic placeholders with brandkit-specific details from the provided brandkit data:
      * Replace "Your Company" or generic company references with the actual brand name from brandkit
      * Replace generic "products/services" with actual services/products mentioned in the brandkit description
      * Replace generic role descriptions with the brandkit's agent_role_description
      * Use the brandkit's conversational_starters as examples where appropriate
      * Reference specific programs, features, or offerings mentioned in the brandkit
    - The agent_role_description from brandkit should be the PRIMARY source for defining the agent's purpose and expertise
    - IMPORTANT: Use ONLY the actual data provided in the brandkit - do not generate or invent brand-specific content
    - Example format: If brandkit mentions specific programs/services, those MUST appear in the realtime_template (use actual names from brandkit)
    - Example format: Replace generic "company" references with the actual company name from brandkit
11. prompt_config.system_instructions: Short behavioral rules aligned with brandkit's agent_role_description
12. outbound_prompt_config: Configuration for outbound calls (when the agent initiates the call)
    - outbound_prompt_config.greeting_template: Generate an outbound greeting that introduces the company and purpose. Should be professional and appropriate for outbound calls. Use the company name from brandkit. Example format: "Hello! This is [Company Name]. I'm calling to see how we can help you today." or similar variations that match the brand's tone.
    - outbound_prompt_config.realtime_template: MUST be the SAME as prompt_config.realtime_template (reuse the exact same content)
    - outbound_prompt_config.system_instructions: MUST be the SAME as prompt_config.system_instructions (reuse the exact same content)

IMPORTANT:
- The 'realtime_template' must be the Voice Instructions template structure, but with ALL generic terms replaced by brandkit-specific information
- Every mention of services, products, or company should reference the actual brandkit data
- The agent_role_description is the authoritative source for the agent's role - use it extensively
- For outbound_prompt_config: reuse realtime_template and system_instructions from prompt_config, but generate a unique outbound greeting_template
- Return ONLY a valid JSON object.
`
	templateInstructions := template.Instructions
	if template.VoiceInstructions != "" {
		templateInstructions = template.VoiceInstructions
	}

	userPrompt := fmt.Sprintf(`Generate a complete voice agent configuration by combining the brandkit data with the voice instructions template.

BRANDKIT DATA (Use this as the PRIMARY source for brand-specific details):
%s

KEY REQUIREMENTS:
1. The agent's role MUST be based on: "%s"
2. Company name is: %s
3. Services/Programs to mention: Extract from the description and longDescription above
4. Welcome message to use: "%s"
5. Conversational starters: %s

VOICE INSTRUCTIONS TEMPLATE (Keep the structure, but replace generic content with brandkit details):
%s

CRITICAL: In the realtime_template, replace:
- Generic company references → "%s" (use the actual company name from brandkit)
- Generic services/products → Actual services from brandkit (extract from description/longDescription, use exact names mentioned)
- Generic role description → The agent_role_description provided above
- Generic examples → Use the conversational_starters and brandkit context

The final realtime_template should read as if it was written specifically for %s, not a generic company.

OUTBOUND CONFIGURATION:
- Generate an outbound greeting_template that introduces %s and explains the purpose of the call
- The outbound greeting should be professional and match the brand's tone
- Reuse the same realtime_template and system_instructions for outbound_prompt_config as in prompt_config`,
		description,
		brandkit.AgentBrandkitConfig.AgentRoleDescription,
		brandkit.Name,
		brandkit.AgentBrandkitConfig.WelcomeMessage,
		strings.Join(brandkit.AgentBrandkitConfig.ConversationalStarters, ", "),
		templateInstructions,
		brandkit.Name,
		brandkit.Name)

	responseText, err := CallOpenAI(apiKey, systemPrompt, userPrompt)
	if err != nil {
		return nil, err
	}

	cleanedJSON := cleanMarkdownJSON(responseText)

	var config domain.AgentConfigData
	if err := json.Unmarshal([]byte(cleanedJSON), &config); err != nil {
		logger.Base().Error("Failed to parse JSON, raw response", zap.String("responsetext", responseText))
		logger.Base().Warn("Cleaned JSON", zap.String("cleanedjson", cleanedJSON))
		return nil, fmt.Errorf("failed to parse agent config JSON: %w", err)
	}

	// Fallback: If AI didn't generate outbound_prompt_config, create it by reusing realtime_template
	if config.OutboundPromptConfig == nil && config.PromptConfig != nil && config.PromptConfig.RealtimeTemplate != "" {
		outboundGreeting := fmt.Sprintf("Hello! This is %s. I'm calling to see how we can help you today.", brandkit.Name)
		config.OutboundPromptConfig = &domain.PromptConfigData{
			GreetingTemplate:   outboundGreeting,
			RealtimeTemplate:   config.PromptConfig.RealtimeTemplate,
			SystemInstructions: config.PromptConfig.SystemInstructions,
		}
	}

	return &config, nil
}

// GenerateConfigFromTemplateOnly generates agent config using AI based on template voice instructions only (No Brandkit)
func GenerateConfigFromTemplateOnly(apiKey string, template *mapping.TemplateV2) (*domain.AgentConfigData, error) {
	logger.Base().Debug("Generating config from template only")
	systemPrompt := `You are an AI assistant that generates complete voice agent configurations based on voice instructions.
Based ONLY on the provided Voice Instructions, generate a comprehensive agent configuration in JSON format.

The Output JSON must strictly follow this structure (matching domain.AgentConfigData):
{
  "persona": "string",
  "tone": "string",
  "language": "string",
  "default_accent": "string",
  "voice": "string",
  "speed": number,
  "services": ["string"],
  "expertise": ["string"],
  "prompt_config": {
    "greeting_template": "string",
    "realtime_template": "string",
    "system_instructions": "string"
  },
  "outbound_prompt_config": {
    "greeting_template": "string",
    "realtime_template": "string",
    "system_instructions": "string"
  }
}

Field Descriptions:
1. persona: The agent's character and role - extract from Voice Instructions, especially from role descriptions or purpose sections
2. tone: Communication style (e.g., professional, friendly, empathetic, warm) - infer from the conversational style described in Voice Instructions
3. language: Primary language code (e.g., "en", "zh", "es") - infer from the language used in Voice Instructions or default to "en"
4. default_accent: Default accent (e.g., "us", "uk", "india") - infer from context or location mentioned, default to "us" for English
5. voice: OpenAI voice name (e.g., alloy, ash, ballad, coral, echo, sage, shimmer, verse, marin, cedar) - choose based on brand personality
6. speed: Speech speed (0.5-2.0, default 1.0)
7. services: Extract specific services/products mentioned in Voice Instructions (e.g., if template mentions "classes", "enrollment", "support", extract those)
8. expertise: Extract areas of expertise from Voice Instructions (e.g., "Customer service", "Product consultation", "Technical support")
9. prompt_config.greeting_template: Extract or generate a specific opening greeting from Voice Instructions. If examples are provided, use them as reference.
10. prompt_config.realtime_template: The main system prompt/instructions.
    CRITICAL REQUIREMENTS:
    - This MUST be the EXACT Voice Instructions template provided, with minimal modifications
    - Preserve ALL sections, formatting, structure, and logic flow
    - Only make minor adjustments if needed for clarity or completeness
    - Do NOT add brandkit-specific details (this function is used when no brandkit data is available)
    - Keep all examples, guardrails, and compliance sections intact
11. prompt_config.system_instructions: Extract or summarize core behavioral rules from Voice Instructions (keep it concise, 1-3 sentences)
12. outbound_prompt_config: Configuration for outbound calls (when the agent initiates the call)
    - outbound_prompt_config.greeting_template: Generate an outbound greeting that introduces the company and purpose. Should be professional and appropriate for outbound calls. Use template variable {{.CompanyName}} for company name. Example format: "Hello! This is {{.CompanyName}}. I'm calling to see how we can help you today." or similar variations that match the template's tone.
    - outbound_prompt_config.realtime_template: MUST be the SAME as prompt_config.realtime_template (reuse the exact same content)
    - outbound_prompt_config.system_instructions: MUST be the SAME as prompt_config.system_instructions (reuse the exact same content)

IMPORTANT:
- The Voice Instructions template is the authoritative source - use it as-is for realtime_template
- Extract persona, services, and expertise by analyzing the Voice Instructions content
- For outbound_prompt_config: reuse realtime_template and system_instructions from prompt_config, but generate a unique outbound greeting_template
- Return ONLY a valid JSON object, no markdown formatting.
`

	templateInstructions := template.VoiceInstructions
	if template.Instructions != "" {
		templateInstructions = template.Instructions
	}
	userPrompt := fmt.Sprintf(`Generate a complete voice agent configuration based ONLY on the provided Voice Instructions template.

VOICE INSTRUCTIONS TEMPLATE:
%s

REQUIREMENTS:
1. Extract the agent's persona and role from the template
2. Identify services and expertise areas mentioned in the template
3. Use the template AS-IS for realtime_template (preserve structure and content)
4. Generate appropriate greeting_template based on examples or style in the template
5. Extract system_instructions as a concise summary of key behavioral rules
6. Generate outbound_prompt_config:
   - Create an outbound greeting_template suitable for outbound calls (use {{.CompanyName}} template variable)
   - Reuse the same realtime_template and system_instructions from prompt_config

The configuration should reflect the voice instructions template accurately.`, templateInstructions)

	responseText, err := CallOpenAI(apiKey, systemPrompt, userPrompt)
	if err != nil {
		return nil, err
	}

	cleanedJSON := cleanMarkdownJSON(responseText)

	var config domain.AgentConfigData
	if err := json.Unmarshal([]byte(cleanedJSON), &config); err != nil {
		logger.Base().Error("Failed to parse JSON, raw response", zap.String("responsetext", responseText))
		logger.Base().Warn("Cleaned JSON", zap.String("cleanedjson", cleanedJSON))
		return nil, fmt.Errorf("failed to parse agent config JSON: %w", err)
	}

	// Fallback: If AI didn't generate outbound_prompt_config, create it by reusing realtime_template
	if config.OutboundPromptConfig == nil && config.PromptConfig != nil && config.PromptConfig.RealtimeTemplate != "" {
		config.OutboundPromptConfig = &domain.PromptConfigData{
			GreetingTemplate:   "Hello! This is {{.CompanyName}}. I'm calling to see how we can help you today.",
			RealtimeTemplate:   config.PromptConfig.RealtimeTemplate,
			SystemInstructions: config.PromptConfig.SystemInstructions,
		}
	}

	return &config, nil
}
