package prompts

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"text/template"

	"github.com/ClareAI/astra-voice-service/internal/config"
	"github.com/ClareAI/astra-voice-service/internal/domain"
	"github.com/ClareAI/astra-voice-service/internal/services/agent"
)

// AgentPromptGenerator generates prompts based on agent configuration
type AgentPromptGenerator struct {
	Agent *config.AgentConfig
}

// NewAgentPromptGenerator creates a new agent-based prompt generator
func NewAgentPromptGenerator(agent *config.AgentConfig) *AgentPromptGenerator {
	return &AgentPromptGenerator{
		Agent: agent,
	}
}

// ==========================================
// Public Interface Methods (config.PromptGenerator)
// ==========================================

// GenerateGreetingInstruction generates agent-specific greeting instructions for inbound calls
func (g *AgentPromptGenerator) GenerateGreetingInstruction(contactName, contactNumber, language, accent string) string {
	effectiveConfig := g.getEffectivePromptConfig(false)
	if effectiveConfig == nil {
		return PromptDefaultGreeting
	}

	var greetingText string
	if effectiveConfig.GreetingTemplate != "" {
		greetingText = g.renderTemplate("greeting", effectiveConfig.GreetingTemplate, contactName, contactNumber)
	}

	return joinBlocks(
		g.generateFirstMessageInstruction(greetingText, language, accent),
		PromptLanguageInstructions,
		g.generateContactInstructions(contactName, contactNumber),
	)
}

// GenerateOutboundGreeting generates self-introduction greeting for outbound calls
func (g *AgentPromptGenerator) GenerateOutboundGreeting(contactName, contactNumber, language, accent string) string {
	effectiveConfig := g.getEffectivePromptConfig(true)
	if effectiveConfig == nil {
		return PromptDefaultOutboundGreeting
	}

	greetingText := g.renderTemplate("outbound_greeting", effectiveConfig.GreetingTemplate, contactName, contactNumber)

	return joinBlocks(
		g.generateFirstMessageInstruction(greetingText, language, accent),
		PromptLanguageInstructions,
		g.generateContactInstructions(contactName, contactNumber),
	)
}

// GenerateSessionInstructions generates comprehensive session-level instructions
func (g *AgentPromptGenerator) GenerateSessionInstructions(contactNumber, language, accent string, isOutbound bool) string {
	effectiveConfig := g.getEffectivePromptConfig(isOutbound)
	if effectiveConfig == nil {
		return PromptSessionFallbackGreeting
	}

	return joinBlocks(
		g.generateRealtimeTemplate(contactNumber, effectiveConfig),
		g.generateAccentContext(effectiveConfig, language, accent),
		g.generateLanguageContext(language, effectiveConfig),
		PromptPhoneConversationRules,
		PromptGreetingRepetitionPrevention,
		g.generateContactInstructions("", contactNumber),
	)
}

// GenerateRealtimeInstruction is a simplified version for backward compatibility
func (g *AgentPromptGenerator) GenerateRealtimeInstruction(contactNumber string) string {
	effectiveConfig := g.getEffectivePromptConfig(false)
	if effectiveConfig == nil {
		return PromptSessionFallbackGreeting
	}

	return joinBlocks(
		g.generateRealtimeTemplate(contactNumber, effectiveConfig),
		PromptPhoneConversationRules,
		g.generateContactInstructions("", contactNumber),
	)
}

// GenerateAccentInstruction delegates to internal accent context builder
func (g *AgentPromptGenerator) GenerateAccentInstruction() string {
	return g.generateAccentContext(g.Agent.PromptConfig, g.Agent.Language, g.Agent.DefaultAccent)
}

// ==========================================
// Meta & Utility Methods
// ==========================================

func (g *AgentPromptGenerator) GetAgentID() string {
	return g.safeGet(func() string { return g.Agent.ID })
}
func (g *AgentPromptGenerator) GetAgentName() string {
	return g.safeGet(func() string { return g.Agent.Name })
}
func (g *AgentPromptGenerator) GetCompanyName() string {
	return g.safeGet(func() string { return g.Agent.CompanyName })
}

func (g *AgentPromptGenerator) safeGet(fn func() string) string {
	if g.Agent == nil {
		return ""
	}
	return fn()
}

// ==========================================
// Internal Building Blocks
// ==========================================

func (g *AgentPromptGenerator) getEffectivePromptConfig(isOutbound bool) *config.PromptConfig {
	if g.Agent == nil {
		return nil
	}
	if isOutbound && g.Agent.OutboundPromptConfig != nil {
		return g.Agent.OutboundPromptConfig
	}
	return g.Agent.PromptConfig
}

func (g *AgentPromptGenerator) renderTemplate(name, tmplStr, contactName, contactNumber string) string {
	if tmplStr == "" {
		return ""
	}
	tmpl, err := template.New(name).Parse(tmplStr)
	if err != nil {
		return g.replaceVariables(tmplStr, contactName, contactNumber)
	}

	var result strings.Builder
	data := map[string]string{
		"ContactName":   contactName,
		"ContactNumber": contactNumber,
		"AgentName":     g.Agent.Name,
		"CompanyName":   g.Agent.CompanyName,
		"Industry":      g.Agent.Industry,
	}

	if err := tmpl.Execute(&result, data); err != nil {
		return g.replaceVariables(tmplStr, contactName, contactNumber)
	}
	return result.String()
}

func (g *AgentPromptGenerator) generateRealtimeTemplate(contactNumber string, promptConfig *config.PromptConfig) string {
	if promptConfig == nil || promptConfig.RealtimeTemplate == "" {
		return ""
	}
	return g.renderTemplate("realtime", promptConfig.RealtimeTemplate, "", contactNumber)
}

func (g *AgentPromptGenerator) generateFirstMessageInstruction(greetingText, language, accent string) string {
	var instruction string

	if greetingText != "" {
		var initialScript string
		if language != "" {
			initialScript = fmt.Sprintf(PromptInitialScriptFlexible, language, language, greetingText)
		} else {
			initialScript = fmt.Sprintf(PromptInitialScriptStrict, greetingText)
		}
		instruction = fmt.Sprintf(PromptInitialGreetingScript, initialScript)
	} else if language != "" {
		// If no greeting script, provide a default task and language requirement
		instruction = fmt.Sprintf(PromptInitialNoScriptRequirement, language)
	}

	// Add default accent instruction if configured
	initialAccent := accent
	if initialAccent == "" {
		initialAccent = g.Agent.DefaultAccent
	}

	if initialAccent != "" {
		// Determine which language to use for accent lookup
		lookupLang := language
		if lookupLang == "" {
			lookupLang = g.Agent.Language
		}

		accentDetail := config.GetAccentDetailedInstruction(lookupLang, strings.ToLower(initialAccent))
		if accentDetail != "" {
			instruction = joinBlocks(instruction, fmt.Sprintf(PromptInitialAccentInstruction, accentDetail))
		}
	}

	return instruction
}

func (g *AgentPromptGenerator) generateContactInstructions(contactName, contactNumber string) string {
	var blocks []string
	if contactName != "" {
		blocks = append(blocks, fmt.Sprintf(PromptContactNameInstruction, contactName, contactName, contactName))
	}
	if contactNumber != "" {
		blocks = append(blocks, fmt.Sprintf(PromptContactNumberInstruction, contactNumber))
	}
	return joinBlocks(blocks...)
}

func (g *AgentPromptGenerator) generateLanguageContext(webhookLanguage string, promptConfig *config.PromptConfig) string {
	if promptConfig == nil {
		return ""
	}

	if !promptConfig.IsAutoLanguageSwitchingEnabled() {
		fixedLang := webhookLanguage
		if fixedLang == "" {
			fixedLang = g.Agent.Language
		}
		if fixedLang == "" {
			fixedLang = "English"
		}

		prompt := PromptFixedLanguageMode
		if promptConfig.IsAutoAccentAdaptationEnabled() {
			prompt += PromptAccentAdaptationMinimal
		}
		return strings.ReplaceAll(prompt, "{LANG}", fixedLang)
	}

	// Dynamic mode
	var languageHint string
	if webhookLanguage != "" {
		languageHint = fmt.Sprintf(PromptInitialLanguageHint, webhookLanguage)
	}

	autoAccentEnabled := promptConfig.IsAutoAccentAdaptationEnabled()
	var sequenceSteps, accentNote, examples string

	if autoAccentEnabled {
		sequenceSteps = PromptLanguageSwitchSequenceAutoAccent
		accentNote = PromptLanguageSwitchNoteAutoAccent
		examples = PromptLanguageSwitchExamplesAutoAccent
	} else {
		sequenceSteps = PromptLanguageSwitchSequenceStandard
		examples = PromptLanguageSwitchExampleStandard
	}

	return fmt.Sprintf(PromptDynamicLanguageSwitching, sequenceSteps, accentNote, examples, languageHint)
}

func (g *AgentPromptGenerator) generateAccentContext(promptConfig *config.PromptConfig, language, accent string) string {
	if promptConfig == nil {
		return ""
	}

	var blocks []string

	// 1. Determine effective accent based on priority: Param > Default > Neutral
	effectiveAccent := accent
	if effectiveAccent == "" {
		effectiveAccent = g.Agent.DefaultAccent
	}

	// If we have a specific accent locked, inject the override hint at the top
	if effectiveAccent != "" {
		// Use detailed instruction if available
		displayAccent := effectiveAccent
		if detail := config.GetAccentDetailedInstruction(language, strings.ToLower(effectiveAccent)); detail != "" {
			displayAccent = detail
		}
		blocks = append(blocks, fmt.Sprintf(PromptCurrentAccentOverride, displayAccent, language))
	}

	accentMap := make(map[string]string)
	if promptConfig.LanguageInstructions != nil {
		for k, v := range promptConfig.LanguageInstructions {
			accentMap[k] = v
		}
	}

	if len(accentMap) == 0 {
		if promptConfig.IsAutoAccentAdaptationEnabled() {
			blocks = append(blocks, PromptAccentAdaptationFull)
		} else {
			blocks = append(blocks, PromptFixedAccentMode)
		}
		return joinBlocks(blocks...)
	}

	// Build detailed accent configuration from map
	var details []string
	processed := make(map[string]bool)
	multiAccentMap := make(map[string][]string)

	for lang, val := range accentMap {
		opts := splitAccentList(val)
		if len(opts) == 0 {
			continue
		}
		if len(opts) > 1 {
			multiAccentMap[lang] = opts
		}

		for _, opt := range opts {
			key := fmt.Sprintf("%s:%s", lang, strings.ToLower(opt))
			if !processed[key] {
				processed[key] = true
				if d := config.GetAccentDetailedInstruction(lang, strings.ToLower(opt)); d != "" {
					details = append(details, d)
				}
			}
		}
	}

	if len(details) == 0 {
		return joinBlocks(blocks...)
	}

	commonRules := PromptConfiguredAccentCommonRules
	if len(multiAccentMap) > 0 {
		commonRules += fmt.Sprintf(PromptMultiAccentLanguageRule, formatLanguageAccentSummary(multiAccentMap))
	}

	var modeRules string
	if promptConfig.IsAutoAccentAdaptationEnabled() {
		modeRules = PromptUnlistedLanguagesAdaptation
	} else {
		if len(multiAccentMap) > 0 {
			modeRules = PromptFixedModeSingleAccent
			modeRules += fmt.Sprintf(PromptMultiAccentFixedMode, formatLanguageAccentSummary(multiAccentMap))
		} else {
			modeRules = PromptFixedModeNoChange
		}
	}

	blocks = append(blocks, fmt.Sprintf(PromptAccentConfigWrapper, strings.Join(details, "\n\n"), commonRules, modeRules))
	return joinBlocks(blocks...)
}

func (g *AgentPromptGenerator) replaceVariables(template, contactName, contactNumber string) string {
	r := template
	r = strings.ReplaceAll(r, "{{.ContactName}}", contactName)
	r = strings.ReplaceAll(r, "{{.ContactNumber}}", contactNumber)
	r = strings.ReplaceAll(r, "{{.AgentName}}", g.Agent.Name)
	r = strings.ReplaceAll(r, "{{.CompanyName}}", g.Agent.CompanyName)
	r = strings.ReplaceAll(r, "{{.Industry}}", g.Agent.Industry)
	return r
}

// joinBlocks cleans and joins multiple prompt blocks with double newlines.
// It trims whitespace from each block and skips empty ones.
func joinBlocks(blocks ...string) string {
	var validBlocks []string
	for _, b := range blocks {
		trimmed := strings.TrimSpace(b)
		if trimmed != "" {
			validBlocks = append(validBlocks, trimmed)
		}
	}
	return strings.Join(validBlocks, "\n\n")
}

func splitAccentList(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	seen := make(map[string]bool)
	var res []string
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t != "" && !seen[strings.ToLower(t)] {
			seen[strings.ToLower(t)] = true
			res = append(res, t)
		}
	}
	return res
}

func formatLanguageAccentSummary(data map[string][]string) string {
	var entries []string
	for lang, accents := range data {
		entries = append(entries, fmt.Sprintf("%s (%s)", lang, strings.Join(accents, ", ")))
	}
	sort.Strings(entries)
	return strings.Join(entries, "; ")
}

// MultiAgentPromptManager manages multiple agents and their prompt generators
type MultiAgentPromptManager struct {
	agentService *agent.AgentService
}

// NewMultiAgentPromptManager creates a new multi-agent prompt manager
func NewMultiAgentPromptManager(agentService *agent.AgentService) *MultiAgentPromptManager {
	return &MultiAgentPromptManager{
		agentService: agentService,
	}
}

// GetPromptGenerator returns a prompt generator for the specified agent
func (m *MultiAgentPromptManager) GetPromptGenerator(agentID string) (config.PromptGenerator, error) {
	return m.GetPromptGeneratorWithChannelType(agentID, "")
}

// GetPromptGeneratorWithChannelType returns a prompt generator for the specified agent with ChannelType support
func (m *MultiAgentPromptManager) GetPromptGeneratorWithChannelType(agentID string, channelType domain.ChannelType) (config.PromptGenerator, error) {
	agent, err := m.agentService.GetAgentConfigWithChannelType(context.Background(), agentID, channelType)
	if err != nil {
		return nil, fmt.Errorf("failed to load agent %s: %w", agentID, err)
	}
	return NewAgentPromptGenerator(agent), nil
}

// GetAllActiveAgents returns all active agents
func (m *MultiAgentPromptManager) GetAllActiveAgents() ([]*config.AgentConfig, error) {
	return m.agentService.GetActiveAgents()
}
