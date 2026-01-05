// Agent Management JavaScript

// Helper function to properly decode newlines and special characters in text
function decodeTextNewlines(text) {
    if (!text) return '';
    // Convert literal \n, \r\n, \t to actual newlines and tabs
    return text.replace(/\\n/g, '\n')
                .replace(/\\r\\n/g, '\n')
                .replace(/\\t/g, '\t');
}

class AgentManager {
    constructor() {
        this.agents = [];
        this.tenants = [];
        this.filteredAgents = [];
        this.currentAgent = null;
        this.isLoading = false;
        
        this.initializeElements();
        this.attachEventListeners();
        this.loadData();
    }

    initializeElements() {
        // Main elements
        this.addAgentBtn = document.getElementById('addAgentBtn');
        this.refreshBtn = document.getElementById('refreshBtn');
        this.searchInput = document.getElementById('searchInput');
        this.tenantFilter = document.getElementById('tenantFilter');
        this.typeFilter = document.getElementById('typeFilter');
        this.includeDisabledFilter = document.getElementById('includeDisabledFilter');
        this.sortSelect = document.getElementById('sortSelect');
        this.exportBtn = document.getElementById('exportBtn');

        // Statistics elements
        this.totalAgentsEl = document.getElementById('totalAgents');
        this.activeAgentsEl = document.getElementById('activeAgents');
        this.disabledAgentsEl = document.getElementById('disabledAgents');
        this.uniqueTenantsEl = document.getElementById('uniqueTenants');

        // Table elements
        this.agentsTableBody = document.getElementById('agentsTableBody');
        this.loadingState = document.getElementById('loadingState');
        this.emptyState = document.getElementById('emptyState');

        // Modal elements
        this.agentModal = document.getElementById('agentModal');
        this.agentForm = document.getElementById('agentForm');
        this.modalTitle = document.getElementById('modalTitle');
        this.saveAgentBtn = document.getElementById('saveAgentBtn');
        this.previewPromptBtn = document.getElementById('previewPromptBtn');

        // Form elements - Basic Info
        this.agentIdField = document.getElementById('agentId');
        this.agentNameField = document.getElementById('agentName');
        this.tenantSelectField = document.getElementById('tenantSelect');
        this.instructionField = document.getElementById('instruction');
        this.disabledField = document.getElementById('disabled');
        
        // Form elements - Agent Config
        this.businessNumberField = document.getElementById('businessNumber');
        this.personaField = document.getElementById('persona');
        this.servicesField = document.getElementById('services');
        this.toneSelectField = document.getElementById('toneSelect');
        this.toneField = document.getElementById('tone');
        this.languageField = document.getElementById('language');
        this.defaultAccentField = document.getElementById('defaultAccent');
        this.voiceField = document.getElementById('voice');
        this.speedField = document.getElementById('speed');
        this.expertiseField = document.getElementById('expertise');
        this.greetingTemplateField = document.getElementById('greetingTemplate');
        this.realtimeTemplateField = document.getElementById('realtimeTemplate');
        this.systemInstructionsField = document.getElementById('systemInstructions');
        this.conversationFlowField = document.getElementById('conversationFlow');
        this.exampleDialoguesField = document.getElementById('exampleDialogues');
        this.languageInstructionsField = document.getElementById('languageInstructions');
        this.customVariablesField = document.getElementById('customVariables');
        
        // Call Config fields
        this.maxCallDurationField = document.getElementById('maxCallDuration');
        this.inactivityCheckDurationField = document.getElementById('inactivityCheckDuration');
        this.silenceMaxRetriesField = document.getElementById('silenceMaxRetries');
        this.inactivityMessageField = document.getElementById('inactivityMessage');

        // Outbound Prompt Config fields
        this.outboundGreetingTemplateField = document.getElementById('outboundGreetingTemplate');
        this.outboundRealtimeTemplateField = document.getElementById('outboundRealtimeTemplate');
        this.outboundIntegratedActionsField = document.getElementById('outboundIntegratedActions');
        
        // Integrated Actions (Inbound)
        this.integratedActionsField = document.getElementById('integratedActions');
        
        this.ragEnabledField = document.getElementById('ragEnabled');
        this.ragBaseUrlField = document.getElementById('ragBaseUrl');
        this.ragTokenField = document.getElementById('ragToken');
        this.ragWorkflowIdField = document.getElementById('ragWorkflowId');
        this.ragDescriptionField = document.getElementById('ragDescription');
        this.ragHeadersField = document.getElementById('ragHeaders');
        this.ragTimeoutField = document.getElementById('ragTimeout');
        this.ragMaxRetriesField = document.getElementById('ragMaxRetries');
        
        // API Config fields
        this.apiEndpointsField = document.getElementById('apiEndpoints');
        this.apiTokensField = document.getElementById('apiTokens');
        this.apiHeadersField = document.getElementById('apiHeaders');
        
        // Business Rules fields
        this.allowedActionsField = document.getElementById('allowedActions');
        this.requiredFieldsField = document.getElementById('requiredFields');
        this.validationRulesField = document.getElementById('validationRules');
        this.maxConversationTimeField = document.getElementById('maxConversationTime');
        this.workingHoursTimezoneField = document.getElementById('workingHoursTimezone');
        this.workingHoursScheduleField = document.getElementById('workingHoursSchedule');
        this.escalationRulesField = document.getElementById('escalationRules');
        this.functionCallRulesField = document.getElementById('functionCallRules');
        
        // Form elements - JSON Editor
        this.agentConfigJsonField = document.getElementById('agentConfigJson');

        // Preview modal elements
        this.promptPreviewModal = document.getElementById('promptPreviewModal');
        this.previewAgentNameEl = document.getElementById('previewAgentName');
        this.previewAgentTypeEl = document.getElementById('previewAgentType');
        this.previewVoiceEl = document.getElementById('previewVoice');
        this.previewSpeedEl = document.getElementById('previewSpeed');
        this.previewPromptTextEl = document.getElementById('previewPromptText');

        // Delete modal elements
        this.deleteModal = document.getElementById('deleteModal');
        this.deleteAgentNameEl = document.getElementById('deleteAgentName');
        this.confirmDeleteBtn = document.getElementById('confirmDeleteBtn');

        // Publish modal elements (we'll use a simple confirm for now or add modal later)
        
        // Toast container
        this.toastContainer = document.getElementById('toastContainer');
    }

    attachEventListeners() {
        // Main actions
        const quickCreateBtn = document.getElementById('quickCreateAgentBtn');
        if (quickCreateBtn) {
            quickCreateBtn.addEventListener('click', () => openQuickCreateModal());
        }
        this.addAgentBtn.addEventListener('click', () => this.openAgentModal());
        this.refreshBtn.addEventListener('click', () => this.loadData());
        this.exportBtn.addEventListener('click', () => this.exportAgents());

        // Filters and search
        this.searchInput.addEventListener('input', () => this.filterAgents());
        this.tenantFilter.addEventListener('change', () => this.filterAgents());
        this.typeFilter.addEventListener('change', () => this.filterAgents());
        this.includeDisabledFilter.addEventListener('change', () => this.filterAgents());
        this.sortSelect.addEventListener('change', () => this.sortAgents());

        // Form elements - only attach if elements exist
        if (this.previewPromptBtn) {
            this.previewPromptBtn.addEventListener('click', () => this.showPromptPreview());
        }

        // Form submission
        this.agentForm.addEventListener('submit', (e) => this.handleFormSubmit(e));

        // Language change handler to update accent options
        if (this.languageField) {
            this.languageField.addEventListener('change', () => this.updateAccentOptions());
        }

        // Delete confirmation
        this.confirmDeleteBtn.addEventListener('click', () => this.deleteAgent());

        // Modal close handlers
        document.addEventListener('click', (e) => {
            if (e.target.classList.contains('modal-overlay')) {
                this.closeModals();
            }
        });

        // Keyboard shortcuts
        document.addEventListener('keydown', (e) => {
            if (e.key === 'Escape') {
                this.closeModals();
            }
        });
    }

    async loadData() {
        this.setLoading(true);
        
        try {
            // Load tenants and agents in parallel
            const [tenantsResponse, agentsResponse] = await Promise.all([
                apiClient.get('/api/tenants?include_disabled=true'),
                apiClient.get(`/api/agents?include_disabled=${this.includeDisabledFilter.checked}`)
            ]);
            
            if (!tenantsResponse.ok || !agentsResponse.ok) {
                throw new Error('Failed to load data');
            }
            
            const rawTenants = await tenantsResponse.json();
            const rawAgents = await agentsResponse.json();
            
            // Transform tenants data
            this.tenants = rawTenants.map(tenant => ({
                ...tenant,
                name: tenant.tenant_name || tenant.name || '',
                is_enabled: tenant.disabled !== undefined ? !tenant.disabled : true
            }));
            
            // Transform agents data
            this.agents = rawAgents.map(agent => this.transformAgentData(agent));
            
            this.populateTenantFilters();
            this.filterAgents();
            this.updateStatistics();
            this.showToast('Data loaded successfully', 'success');
            
        } catch (error) {
            console.error('Error loading data:', error);
            this.showToast('Failed to load data', 'error');
            this.agents = [];
            this.tenants = [];
            this.filteredAgents = [];
        } finally {
            this.setLoading(false);
        }
    }

    transformAgentData(agent) {
        // Extract config from agent_config
        const config = agent.agent_config || {};
        
        // After migration: voice_tenant_id is now VARCHAR (business ID like 'wati', 'peak-capital')
        // Find tenant by business ID (tenant_id) instead of UUID (id)
        const tenant = this.tenants.find(t => t.tenant_id === agent.voice_tenant_id || t.id === agent.voice_tenant_id);
        
        return {
            ...agent,
            name: agent.agent_name || agent.name || '',
            tenant_business_id: agent.voice_tenant_id || agent.tenant_id,  // Business ID (e.g., 'wati')
            tenant_uuid: tenant ? tenant.id : 'N/A',  // UUID of the tenant
            tenant_id: tenant ? tenant.id : agent.voice_tenant_id,  // For backwards compatibility
            tenant_name: tenant ? (tenant.name || tenant.tenant_name) : 'Unknown',
            type: config.business_type || config.type || 'custom',
            voice: config.voice || agent.voice || 'alloy',
            speed: config.speed !== undefined ? config.speed : (agent.speed !== undefined ? agent.speed : 1.0),
            language: config.language || agent.language || 'en',
            business_number: config.business_number || '',
            system_prompt: agent.instruction || agent.system_prompt || '',
            description: this.extractAgentDescription(agent, config),
            is_enabled: agent.disabled !== undefined ? !agent.disabled : true
        };
    }

    extractAgentDescription(agent, config) {
        if (agent.description) return agent.description;
        if (config.description) return config.description;
        if (config.persona) return config.persona;
        return '';
    }

    populateTenantFilters() {
        // Populate tenant filter dropdown
        // After migration: Use tenant_id (business ID) for filtering
        this.tenantFilter.innerHTML = '<option value="">All Tenants</option>';
        this.tenants.forEach(tenant => {
            const option = document.createElement('option');
            option.value = tenant.tenant_id || tenant.id;  // Use business ID
            option.textContent = tenant.name;
            this.tenantFilter.appendChild(option);
        });

        // Populate tenant select in form
        // After migration: Use tenant_id (business ID) as the value to be submitted
        this.tenantSelectField.innerHTML = '<option value="">Select tenant</option>';
        this.tenants.filter(t => t.is_enabled).forEach(tenant => {
            const option = document.createElement('option');
            option.value = tenant.tenant_id || tenant.id;  // Use business ID
            option.textContent = tenant.name;
            this.tenantSelectField.appendChild(option);
        });
    }

    filterAgents() {
        const searchTerm = this.searchInput.value.toLowerCase();
        const selectedTenant = this.tenantFilter.value;
        const selectedType = this.typeFilter.value;
        const includeDisabled = this.includeDisabledFilter.checked;

        this.filteredAgents = this.agents.filter(agent => {
            // Search filter - search in name, type, tenant name, IDs, and business number
            const matchesSearch = !searchTerm || 
                agent.name.toLowerCase().includes(searchTerm) ||
                agent.type.toLowerCase().includes(searchTerm) ||
                (agent.tenant_name && agent.tenant_name.toLowerCase().includes(searchTerm)) ||
                (agent.tenant_business_id && agent.tenant_business_id.toLowerCase().includes(searchTerm)) ||
                (agent.tenant_uuid && agent.tenant_uuid.toLowerCase().includes(searchTerm)) ||
                (agent.id && agent.id.toLowerCase().includes(searchTerm)) ||
                (agent.tenant_id && agent.tenant_id.toLowerCase().includes(searchTerm)) ||
                (agent.voice_tenant_id && agent.voice_tenant_id.toLowerCase().includes(searchTerm)) ||
                (agent.business_number && agent.business_number.toLowerCase().includes(searchTerm));

            // Tenant filter
            // After migration: Match against tenant_business_id (business ID)
            const matchesTenant = !selectedTenant || 
                agent.tenant_business_id === selectedTenant || 
                agent.voice_tenant_id === selectedTenant ||
                agent.tenant_id === selectedTenant;

            // Type filter
            const matchesType = !selectedType || agent.type === selectedType;

            // Status filter
            const matchesStatus = includeDisabled || agent.is_enabled;

            return matchesSearch && matchesTenant && matchesType && matchesStatus;
        });

        this.sortAgents();
    }

    sortAgents() {
        const sortBy = this.sortSelect.value;
        
        this.filteredAgents.sort((a, b) => {
            switch (sortBy) {
                case 'name':
                    return a.name.localeCompare(b.name);
                case 'type':
                    return a.type.localeCompare(b.type);
                case 'created_at':
                    return new Date(b.created_at) - new Date(a.created_at);
                case 'updated_at':
                    return new Date(b.updated_at) - new Date(a.updated_at);
                default:
                    return a.name.localeCompare(b.name);
            }
        });

        this.renderAgents();
    }

    renderAgents() {
        if (this.filteredAgents.length === 0) {
            this.agentsTableBody.innerHTML = '';
            this.emptyState.style.display = 'block';
            return;
        }

        this.emptyState.style.display = 'none';
        
        this.agentsTableBody.innerHTML = this.filteredAgents.map(agent => `
            <tr>
                <td>
                    <div style="font-weight: 600; color: #374151;">${this.escapeHtml(agent.name)}</div>
                    ${agent.description ? `<div style="font-size: 0.8rem; color: #6b7280; margin-top: 2px;">${this.escapeHtml(agent.description)}</div>` : ''}
                </td>
                <td>
                    <code style="background: #f3f4f6; padding: 2px 6px; border-radius: 4px; font-size: 0.75rem; display: block; overflow: hidden; text-overflow: ellipsis; max-width: 150px;" title="${this.escapeHtml(agent.id)}">
                        ${this.escapeHtml(agent.id.substring(0, 8))}...
                    </code>
                </td>
                <td>
                    <span class="type-badge type-${agent.type}">
                        ${this.formatAgentType(agent.type)}
                    </span>
                </td>
                <td>
                    <div style="font-weight: 500; color: #374151;">${this.escapeHtml(agent.tenant_name || 'Unknown')}</div>
                </td>
                <td>
                    <div style="display: flex; flex-direction: column; gap: 4px;">
                        <code style="background: #dbeafe; padding: 2px 6px; border-radius: 4px; font-size: 0.8rem; color: #1e40af; font-weight: 500;" title="Business ID">
                            ${this.escapeHtml(agent.tenant_business_id || 'N/A')}
                        </code>
                        <code style="background: #f3f4f6; padding: 2px 6px; border-radius: 4px; font-size: 0.7rem; color: #6b7280;" title="UUID: ${this.escapeHtml(agent.tenant_uuid || 'N/A')}">
                            ${this.escapeHtml((agent.tenant_uuid || 'N/A').substring(0, 8))}...
                        </code>
                    </div>
                </td>
                <td>
                    ${agent.business_number ? `
                        <code style="background: #fef3c7; padding: 2px 6px; border-radius: 4px; font-size: 0.85rem; color: #92400e; font-weight: 500;">
                            ${this.escapeHtml(agent.business_number)}
                        </code>
                    ` : '<span style="color: #9ca3af; font-size: 0.85rem;">-</span>'}
                </td>
                <td>
                    <div class="voice-info">
                        <span class="voice-name">${this.escapeHtml(agent.voice || 'alloy')}</span>
                        <span class="voice-speed">${agent.speed || 1.0}x</span>
                    </div>
                </td>
                <td>
                    <span class="status-badge ${agent.is_enabled ? 'status-active' : 'status-disabled'}">
                        ${agent.is_enabled ? 'Active' : 'Disabled'}
                    </span>
                </td>
                <td>
                    <div style="font-size: 0.85rem; color: #374151;">
                        ${this.formatDate(agent.created_at)}
                    </div>
                </td>
                <td>
                    <div class="action-buttons">
                        <button class="action-btn action-btn-view" onclick="agentManager.viewAgent('${agent.id}')" title="View Details">
                            👁️ View
                        </button>
                        <button class="action-btn action-btn-edit" onclick="agentManager.editAgent('${agent.id}')" title="Edit Agent">
                            ✏️ Edit
                        </button>
                        <button class="action-btn action-btn-jwt" onclick="agentManager.showJWTForAgent('${agent.id}')" title="Show JWT Token">
                            🔑 JWT
                        </button>
                        <button class="action-btn action-btn-publish" onclick="agentManager.publishAgent('${agent.id}')" title="Publish Agent">
                            🚀 Publish
                        </button>
                        <button class="action-btn action-btn-delete" onclick="agentManager.confirmDeleteAgent('${agent.id}')" title="Delete Agent">
                            🗑️ Delete
                        </button>
                    </div>
                </td>
            </tr>
        `).join('');
    }

    updateStatistics() {
        const totalAgents = this.agents.length;
        const activeAgents = this.agents.filter(a => a.is_enabled).length;
        const disabledAgents = totalAgents - activeAgents;
        const uniqueTenants = new Set(this.agents.map(a => a.tenant_id)).size;

        this.totalAgentsEl.textContent = totalAgents;
        this.activeAgentsEl.textContent = activeAgents;
        this.disabledAgentsEl.textContent = disabledAgents;
        this.uniqueTenantsEl.textContent = uniqueTenants;
    }

    openAgentModal(agent = null) {
        this.currentAgent = agent;
        
        if (agent) {
            // Edit mode
            this.modalTitle.textContent = 'Edit Agent';
            this.agentIdField.value = agent.id;
            this.agentNameField.value = agent.agent_name || agent.name || '';
            this.tenantSelectField.value = agent.voice_tenant_id || '';
            this.instructionField.value = agent.instruction || '';
            this.disabledField.checked = agent.disabled || false;
            
            // Populate agent_config fields
            const config = agent.agent_config || {};
            this.businessNumberField.value = config.business_number || '';
            this.personaField.value = config.persona || '';
            this.servicesField.value = (config.services || []).join(', ');
            
            // Handle tone field - check if it's a preset value or custom
            const toneValue = config.tone || '';
            const presetTones = ['friendly', 'professional', 'casual', 'enthusiastic', 'empathetic', 'warm', 'formal', 'conversational'];
            if (presetTones.includes(toneValue)) {
                // It's a preset value
                this.toneSelectField.value = toneValue;
                this.toneField.value = toneValue;
                this.toneField.style.display = 'none';
                const toneHelp = document.getElementById('toneHelp');
                if (toneHelp) toneHelp.style.display = 'none';
            } else if (toneValue) {
                // It's a custom value
                this.toneSelectField.value = '__custom__';
                this.toneField.value = toneValue;
                this.toneField.style.display = 'block';
                const toneHelp = document.getElementById('toneHelp');
                if (toneHelp) toneHelp.style.display = 'block';
            } else {
                // No value
                this.toneSelectField.value = '';
                this.toneField.value = '';
                this.toneField.style.display = 'none';
                const toneHelp = document.getElementById('toneHelp');
                if (toneHelp) toneHelp.style.display = 'none';
            }
            
            this.languageField.value = config.language || 'en';
            // Update accent options based on language, then set value
            this.updateAccentOptions();
            this.defaultAccentField.value = config.default_accent || '';
            this.voiceField.value = config.voice || '';
            const speedValue = config.speed || 1.0;
            this.speedField.value = speedValue;
            // Update speed display if function exists
            if (typeof window.updateSpeedDisplay === 'function') {
                window.updateSpeedDisplay(speedValue);
            }
            this.expertiseField.value = (config.expertise || []).join(', ');

            // Populate Call Config
            this.maxCallDurationField.value = config.max_call_duration || '';
            if (config.silence_config) {
                this.inactivityCheckDurationField.value = config.silence_config.inactivity_check_duration || '';
                this.silenceMaxRetriesField.value = config.silence_config.max_retries || '';
                this.inactivityMessageField.value = config.silence_config.inactivity_message || '';
            } else {
                this.inactivityCheckDurationField.value = '';
                this.silenceMaxRetriesField.value = '';
                this.inactivityMessageField.value = '';
            }
            
            // Populate prompt_config fields
            if (config.prompt_config) {
                this.greetingTemplateField.value = decodeTextNewlines(config.prompt_config.greeting_template || '');
                this.realtimeTemplateField.value = decodeTextNewlines(config.prompt_config.realtime_template || '');
                this.systemInstructionsField.value = decodeTextNewlines(config.prompt_config.system_instructions || '');
                this.conversationFlowField.value = (config.prompt_config.conversation_flow || []).join(', ');
                this.exampleDialoguesField.value = config.prompt_config.example_dialogues ? JSON.stringify(config.prompt_config.example_dialogues, null, 2) : '';
                this.languageInstructionsField.value = config.prompt_config.language_instructions ? JSON.stringify(config.prompt_config.language_instructions, null, 2) : '';
                this.customVariablesField.value = config.prompt_config.custom_variables ? JSON.stringify(config.prompt_config.custom_variables, null, 2) : '';
                
                // Load language & accent adaptation settings (default: true if not specified)
                const autoLangSwitching = config.prompt_config.auto_language_switching !== undefined 
                    ? config.prompt_config.auto_language_switching 
                    : true;
                const autoAccent = config.prompt_config.auto_accent_adaptation !== undefined 
                    ? config.prompt_config.auto_accent_adaptation 
                    : true;
                
                const autoLanguageSwitchingField = document.getElementById('autoLanguageSwitching');
                const autoAccentAdaptationField = document.getElementById('autoAccentAdaptation');
                if (autoLanguageSwitchingField) autoLanguageSwitchingField.checked = autoLangSwitching;
                if (autoAccentAdaptationField) autoAccentAdaptationField.checked = autoAccent;
            }
            
            // Populate outbound_prompt_config fields
            if (config.outbound_prompt_config) {
                this.outboundGreetingTemplateField.value = decodeTextNewlines(config.outbound_prompt_config.greeting_template || '');
                this.outboundRealtimeTemplateField.value = decodeTextNewlines(config.outbound_prompt_config.realtime_template || '');
            } else {
                this.outboundGreetingTemplateField.value = '';
                this.outboundRealtimeTemplateField.value = '';
            }
            this.outboundIntegratedActionsField.value = config.outbound_integrated_actions ? JSON.stringify(config.outbound_integrated_actions, null, 2) : '';
            
            // Populate Integrated Actions (Inbound)
            this.integratedActionsField.value = config.integrated_actions ? JSON.stringify(config.integrated_actions, null, 2) : '';
            
            // Populate rag_config fields
            if (config.rag_config) {
                this.ragEnabledField.checked = config.rag_config.enabled || false;
                this.ragBaseUrlField.value = config.rag_config.base_url || '';
                this.ragTokenField.value = config.rag_config.token || '';
                this.ragWorkflowIdField.value = config.rag_config.workflow_id || '';
                this.ragDescriptionField.value = config.rag_config.description || '';
                this.ragHeadersField.value = config.rag_config.headers ? JSON.stringify(config.rag_config.headers, null, 2) : '';
                this.ragTimeoutField.value = config.rag_config.timeout || '';
                this.ragMaxRetriesField.value = config.rag_config.max_retries || '';
            }
            
            // Populate API Config
            if (config.api_config) {
                this.apiEndpointsField.value = config.api_config.endpoints ? JSON.stringify(config.api_config.endpoints, null, 2) : '';
                this.apiTokensField.value = config.api_config.tokens ? JSON.stringify(config.api_config.tokens, null, 2) : '';
                this.apiHeadersField.value = config.api_config.headers ? JSON.stringify(config.api_config.headers, null, 2) : '';
            }
            
            // Populate Business Rules
            if (config.business_rules) {
                this.allowedActionsField.value = (config.business_rules.allowed_actions || []).join(', ');
                this.requiredFieldsField.value = (config.business_rules.required_fields || []).join(', ');
                this.validationRulesField.value = config.business_rules.validation_rules ? JSON.stringify(config.business_rules.validation_rules, null, 2) : '';
                this.maxConversationTimeField.value = config.business_rules.max_conversation_time || '';
                
                if (config.business_rules.working_hours) {
                    this.workingHoursTimezoneField.value = config.business_rules.working_hours.timezone || '';
                    this.workingHoursScheduleField.value = config.business_rules.working_hours.schedule ? JSON.stringify(config.business_rules.working_hours.schedule, null, 2) : '';
                }
                
                this.escalationRulesField.value = config.business_rules.escalation_rules ? JSON.stringify(config.business_rules.escalation_rules, null, 2) : '';
                this.functionCallRulesField.value = config.business_rules.function_call_rules ? JSON.stringify(config.business_rules.function_call_rules, null, 2) : '';
            }
            
            // Populate JSON editor
            this.agentConfigJsonField.value = JSON.stringify(config, null, 2);
            
            this.saveAgentBtn.innerHTML = '<span class="btn-icon">💾</span> Update Agent';
        } else {
            // Create mode
            this.modalTitle.textContent = 'Add New Agent';
            this.agentForm.reset();
            this.agentIdField.value = '';
            this.disabledField.checked = false;
            this.agentConfigJsonField.value = '{}';
            // Initialize accent options based on default language
            this.updateAccentOptions();
            this.saveAgentBtn.innerHTML = '<span class="btn-icon">💾</span> Create Agent';
        }
        
        this.agentModal.classList.add('active');
        this.agentNameField.focus();
    }

    closeAgentModal() {
        this.agentModal.classList.remove('active');
        this.currentAgent = null;
        this.agentForm.reset();
    }

    handleAgentTypeChange() {
        // Removed - agent type field no longer exists
        return;
        
        // const selectedType = this.agentTypeField.value;
        
        // Set default prompts based on agent type
        const defaultPrompts = {
            wati: `You are Sarah, a friendly and natural WATI sales representative. Your goal is to have 2-3 rounds of warm, natural conversation before smoothly transitioning to business talk.

🎯 CONVERSATION FLOW (2-3 rounds):
ROUND 1: Natural greeting with light small talk
ROUND 2: Respond to their small talk, then casually mention you're from WATI  
ROUND 3: When they ask or show interest, smoothly transition to business mode

🗣️ HUMAN SPEECH PATTERNS (Use naturally):
- Vocal fillers: "um", "uh", "ah", "er", "hmm", "well", "you know", "like"
- Thinking sounds: "let me see...", "hmm, that's interesting", "oh, actually..."
- Natural hesitations: "I mean...", "well, the thing is...", "so, um..."
- Conversational bridges: "anyway", "by the way", "speaking of which"

🚨 IMPORTANT RULES:
- 👤 ALWAYS use the contact name if provided - say "Hi {{.ContactName}}" instead of "Hi there"
- Start with genuine small talk, NOT business
- Be patient - let the conversation develop naturally
- Only mention WATI after some friendly exchange
- When they show interest, provide concise business information (keep it brief - this is a phone call!)
- 🚫 NEVER ASK FOR WHATSAPP NUMBER: The system already knows it

Remember: Build rapport first, business second. Make it feel like a natural conversation between friends!`,
            
            automotive: `You are Mike, a helpful and knowledgeable automotive consultant at Next Gear Auto who focuses on understanding customer needs first.

🎯 YOUR MAIN GOALS:
1. Understand their automotive needs and preferences
2. Provide helpful vehicle recommendations based on their requirements
3. Offer test drive scheduling when they show interest in specific vehicles
4. Be genuinely helpful rather than pushy

📞 PHONE CONVERSATION GUIDELINES:
- Keep responses concise and conversational
- Focus on understanding their needs: budget, family size, usage, preferences
- DO NOT immediately push for a sale
- Let the conversation flow naturally

🗣️ NATURAL SPEECH PATTERNS (Use frequently):
- Vocal fillers: "um", "uh", "ah", "hmm", "well", "you know", "like"
- Automotive thinking: "Hmm... let me check...", "Well... actually, I think...", "Um... just a moment..."
- Natural reactions: "oh, that's a good choice", "hmm, makes sense", "right, exactly"
- Conversational bridges: "anyway", "by the way", "the thing is", "what I usually recommend is"

Focus on understanding customer needs first, then match them with suitable vehicles and financing options.`,
            
            financial: `You are Emma from Peak Capital Wealth Management. Keep conversations natural and follow the demo flow.

🎯 CONVERSATION FLOW:
1. Opening: Mention Hong Kong fintech opportunities and 18-22% returns
2. Value Prop: Share client success story (24% on HK$5M investment)
3. Qualification: Ask about investment timeline
4. Product Match: Suggest Hong Kong Tech Growth Fund (HK$2M minimum, 4-6% quarterly)
5. Appointment: Offer meeting with David Wong (Thursday 2:30 PM or Friday 10 AM)
6. Booking: Use "book_peak_capital_consultation" function when they agree

🗣️ NATURAL SPEECH PATTERNS (Use frequently):
- Vocal fillers: "um", "uh", "ah", "hmm", "well", "you know", "like"
- Professional thinking: "let me see...", "actually...", "I mean...", "so..."
- Natural reactions: "oh, that's interesting", "hmm, good point", "right, exactly"
- Conversational bridges: "anyway", "by the way", "the thing is", "what I'm seeing is"
- Financial hesitations: "well, the market's been...", "um, our clients typically...", "so, basically..."

💡 KEY POINTS:
- 18-22% returns on Hong Kong Innovation Portfolio
- Recent client: 24% gain on HK$5M investment  
- Hong Kong Tech Growth Fund: HK$2M minimum, 4-6% quarterly returns
- Meeting with David Wong for detailed consultation`,
            
            restaurant: `You are Lily, a professional restaurant hostess and booking specialist for Golden Dragon Restaurant in Hong Kong.

🎯 MAIN OBJECTIVES:
1. Handle restaurant reservations and table bookings efficiently
2. Provide information about menu, dining options, and special occasions
3. Offer appropriate dining upgrades and special services
4. Ensure excellent dining experience and customer satisfaction

⚡ CRITICAL: Never leave conversations hanging! Always provide complete responses with options or next steps. Don't stop mid-conversation after saying "let me check" - immediately continue with results!

🌏 LANGUAGE RULES:
- SINGLE LANGUAGE: Never mix languages in the same response - use only one language at a time
- LANGUAGE DETECTION: Detect the user's preferred language from their first response
- CONSISTENCY: Once language is detected, maintain that language throughout the conversation
- DEFAULT: Start with English greeting, then adapt based on user's response

🏢 RESTAURANT FOCUS:
- You ONLY represent Golden Dragon Restaurant - a premium Chinese restaurant in Hong Kong
- Specialize in Cantonese cuisine, dim sum, and private dining experiences
- Handle table reservations, special occasions, and dining inquiries
- Never represent any other business or industry
- Always maintain your identity as Golden Dragon Restaurant's hostess

Focus on providing excellent restaurant service and booking experience.`,
            
            travel: `You are Sophie, a professional travel specialist for Premier Travel in Hong Kong.

🎯 MAIN OBJECTIVES:
1. Help customers plan and book travel packages to Asia Pacific destinations
2. Provide destination information, travel advice, and cultural insights
3. Handle flight, hotel, and tour bookings efficiently
4. Offer appropriate travel upgrades and premium services

⚡ CRITICAL: Never leave conversations hanging! Always provide complete responses with options or next steps. Don't stop mid-conversation after saying "let me check" - immediately continue with results!

🌏 LANGUAGE RULES:
- SINGLE LANGUAGE: Never mix languages in the same response - use only one language at a time
- LANGUAGE DETECTION: Detect the user's preferred language from their first response
- CONSISTENCY: Once language is detected, maintain that language throughout the conversation
- DEFAULT: Start with English greeting, then adapt based on user's response

✈️ TRAVEL SPECIALIZATION:
- You ONLY represent Premier Travel - specialized in Asia Pacific travel
- Focus on Japan, Korea, Taiwan, Thailand, Singapore, and other regional destinations
- Handle travel packages, flights, hotels, and cultural experiences
- Never represent any other business or industry
- Always maintain your identity as Premier Travel's specialist

Focus on planning perfect Asia Pacific travel experiences with professional travel advice and booking services.`
        };

        // Removed - systemPromptField no longer exists
        // if (defaultPrompts[selectedType] && !this.systemPromptField.value) {
        //     this.systemPromptField.value = defaultPrompts[selectedType];
        // }
    }

    updateSpeedDisplay() {
        // Removed - voice speed field no longer exists
        return;
    }

    showPromptPreview() {
        // Populate preview data
        this.previewAgentNameEl.textContent = this.agentNameField.value || 'Unnamed Agent';
        this.previewAgentTypeEl.textContent = this.personaField.value || 'No Persona';
        this.previewVoiceEl.textContent = this.languageField.value || 'en';
        this.previewSpeedEl.textContent = '1.0x'; // No longer editable in this version
        this.previewPromptTextEl.textContent = this.realtimeTemplateField.value || this.systemInstructionsField.value || 'No prompt entered';

        this.promptPreviewModal.classList.add('active');
    }

    closePromptPreviewModal() {
        this.promptPreviewModal.classList.remove('active');
    }

    async handleFormSubmit(e) {
        e.preventDefault();
        
        // Get basic info
        const agentName = this.agentNameField.value.trim();
        const voiceTenantId = this.tenantSelectField.value;
        const instruction = this.instructionField.value.trim();
        const disabled = this.disabledField.checked;
        
        // Build agent_config object - ALWAYS build from form fields first
        // This ensures form changes are not lost
        let agentConfig = {};
        
        if (this.businessNumberField.value) agentConfig.business_number = this.businessNumberField.value.trim();
        if (this.personaField.value) agentConfig.persona = this.personaField.value.trim();
        if (this.toneField.value) agentConfig.tone = this.toneField.value;
        if (this.languageField.value) agentConfig.language = this.languageField.value;
        if (this.defaultAccentField.value) agentConfig.default_accent = this.defaultAccentField.value;
        if (this.voiceField.value) agentConfig.voice = this.voiceField.value;
        
        // Parse speed value
        const speedValue = parseFloat(this.speedField.value);
        if (!isNaN(speedValue) && speedValue >= 0.25 && speedValue <= 4.0) {
            agentConfig.speed = speedValue;
        }
        
        // Parse comma-separated values
        const services = this.servicesField.value.trim();
        if (services) {
            agentConfig.services = services.split(',').map(s => s.trim()).filter(s => s);
        }
        
        const expertise = this.expertiseField.value.trim();
        if (expertise) {
            agentConfig.expertise = expertise.split(',').map(s => s.trim()).filter(s => s);
        }
        
        // Build prompt_config
        const promptConfig = {};
        if (this.greetingTemplateField.value) promptConfig.greeting_template = this.greetingTemplateField.value;
        if (this.realtimeTemplateField.value) promptConfig.realtime_template = this.realtimeTemplateField.value;
        if (this.systemInstructionsField.value) promptConfig.system_instructions = this.systemInstructionsField.value;
        
        const conversationFlow = this.conversationFlowField.value.trim();
        if (conversationFlow) {
            promptConfig.conversation_flow = conversationFlow.split(',').map(s => s.trim()).filter(s => s);
        }
        
        if (this.exampleDialoguesField.value.trim()) {
            try {
                promptConfig.example_dialogues = JSON.parse(this.exampleDialoguesField.value);
            } catch (e) {
                this.showToast('Invalid JSON in Example Dialogues', 'error');
                return;
            }
        }
        
        if (this.languageInstructionsField.value.trim()) {
            try {
                promptConfig.language_instructions = JSON.parse(this.languageInstructionsField.value);
            } catch (e) {
                this.showToast('Invalid JSON in Language Instructions', 'error');
                return;
            }
        }
        
        if (this.customVariablesField.value.trim()) {
            try {
                promptConfig.custom_variables = JSON.parse(this.customVariablesField.value);
            } catch (e) {
                this.showToast('Invalid JSON in Custom Variables', 'error');
                return;
            }
        }
        
        // Add language & accent adaptation settings
        const autoLanguageSwitchingField = document.getElementById('autoLanguageSwitching');
        const autoAccentAdaptationField = document.getElementById('autoAccentAdaptation');
        
        if (autoLanguageSwitchingField) {
            promptConfig.auto_language_switching = autoLanguageSwitchingField.checked;
        }
        if (autoAccentAdaptationField) {
            promptConfig.auto_accent_adaptation = autoAccentAdaptationField.checked;
        }
        
        if (Object.keys(promptConfig).length > 0) {
            agentConfig.prompt_config = promptConfig;
        }

        // Build call_config
        if (this.maxCallDurationField.value) {
            agentConfig.max_call_duration = parseInt(this.maxCallDurationField.value);
        }

        const silenceConfig = {};
        if (this.inactivityCheckDurationField.value) silenceConfig.inactivity_check_duration = parseInt(this.inactivityCheckDurationField.value);
        if (this.silenceMaxRetriesField.value) silenceConfig.max_retries = parseInt(this.silenceMaxRetriesField.value);
        if (this.inactivityMessageField.value) silenceConfig.inactivity_message = this.inactivityMessageField.value;
        
        if (Object.keys(silenceConfig).length > 0) {
            agentConfig.silence_config = silenceConfig;
        }

        // Build outbound_prompt_config
        const outboundPromptConfig = {};
        if (this.outboundGreetingTemplateField.value) outboundPromptConfig.greeting_template = this.outboundGreetingTemplateField.value;
        if (this.outboundRealtimeTemplateField.value) outboundPromptConfig.realtime_template = this.outboundRealtimeTemplateField.value;
        
        if (Object.keys(outboundPromptConfig).length > 0) {
            agentConfig.outbound_prompt_config = outboundPromptConfig;
        }

        if (this.outboundIntegratedActionsField.value.trim()) {
            try {
                agentConfig.outbound_integrated_actions = JSON.parse(this.outboundIntegratedActionsField.value);
            } catch (e) {
                this.showToast('Invalid JSON in Outbound Integrated Actions', 'error');
                return;
            }
        }
        
        // Integrated Actions (Inbound)
        if (this.integratedActionsField.value.trim()) {
            try {
                agentConfig.integrated_actions = JSON.parse(this.integratedActionsField.value);
            } catch (e) {
                this.showToast('Invalid JSON in Integrated Actions', 'error');
                return;
            }
        }
        
        // Build rag_config
        const ragConfig = {
            enabled: this.ragEnabledField.checked
        };
        if (this.ragBaseUrlField.value) ragConfig.base_url = this.ragBaseUrlField.value.trim();
        if (this.ragTokenField.value) ragConfig.token = this.ragTokenField.value.trim();
        if (this.ragWorkflowIdField.value) ragConfig.workflow_id = this.ragWorkflowIdField.value.trim();
        if (this.ragDescriptionField.value) ragConfig.description = this.ragDescriptionField.value.trim();
        
        if (this.ragHeadersField.value.trim()) {
            try {
                ragConfig.headers = JSON.parse(this.ragHeadersField.value);
            } catch (e) {
                this.showToast('Invalid JSON in RAG Headers', 'error');
                return;
            }
        }
        
        if (this.ragTimeoutField.value) ragConfig.timeout = parseInt(this.ragTimeoutField.value);
        if (this.ragMaxRetriesField.value) ragConfig.max_retries = parseInt(this.ragMaxRetriesField.value);
        
        if (ragConfig.enabled || ragConfig.base_url || ragConfig.token || Object.keys(ragConfig).length > 1) {
            agentConfig.rag_config = ragConfig;
        }
        
        // Build api_config
        const apiConfig = {};
        if (this.apiEndpointsField.value.trim()) {
            try {
                apiConfig.endpoints = JSON.parse(this.apiEndpointsField.value);
            } catch (e) {
                this.showToast('Invalid JSON in API Endpoints', 'error');
                return;
            }
        }
        if (this.apiTokensField.value.trim()) {
            try {
                apiConfig.tokens = JSON.parse(this.apiTokensField.value);
            } catch (e) {
                this.showToast('Invalid JSON in API Tokens', 'error');
                return;
            }
        }
        if (this.apiHeadersField.value.trim()) {
            try {
                apiConfig.headers = JSON.parse(this.apiHeadersField.value);
            } catch (e) {
                this.showToast('Invalid JSON in API Headers', 'error');
                return;
            }
        }
        if (Object.keys(apiConfig).length > 0) {
            agentConfig.api_config = apiConfig;
        }
        
        // Build business_rules
        const businessRules = {};
        
        const allowedActions = this.allowedActionsField.value.trim();
        if (allowedActions) {
            businessRules.allowed_actions = allowedActions.split(',').map(s => s.trim()).filter(s => s);
        }
        
        const requiredFields = this.requiredFieldsField.value.trim();
        if (requiredFields) {
            businessRules.required_fields = requiredFields.split(',').map(s => s.trim()).filter(s => s);
        }
        
        if (this.validationRulesField.value.trim()) {
            try {
                businessRules.validation_rules = JSON.parse(this.validationRulesField.value);
            } catch (e) {
                this.showToast('Invalid JSON in Validation Rules', 'error');
                return;
            }
        }
        
        if (this.maxConversationTimeField.value) {
            businessRules.max_conversation_time = parseInt(this.maxConversationTimeField.value);
        }
        
        // Working Hours
        if (this.workingHoursTimezoneField.value.trim() || this.workingHoursScheduleField.value.trim()) {
            businessRules.working_hours = {};
            if (this.workingHoursTimezoneField.value.trim()) {
                businessRules.working_hours.timezone = this.workingHoursTimezoneField.value.trim();
            }
            if (this.workingHoursScheduleField.value.trim()) {
                try {
                    businessRules.working_hours.schedule = JSON.parse(this.workingHoursScheduleField.value);
                } catch (e) {
                    this.showToast('Invalid JSON in Working Hours Schedule', 'error');
                    return;
                }
            }
        }
        
        // Escalation Rules
        if (this.escalationRulesField.value.trim()) {
            try {
                businessRules.escalation_rules = JSON.parse(this.escalationRulesField.value);
            } catch (e) {
                this.showToast('Invalid JSON in Escalation Rules', 'error');
                return;
            }
        }
        
        // Function Call Rules
        if (this.functionCallRulesField.value.trim()) {
            try {
                businessRules.function_call_rules = JSON.parse(this.functionCallRulesField.value);
            } catch (e) {
                this.showToast('Invalid JSON in Function Call Rules', 'error');
                return;
            }
        }
        
        if (Object.keys(businessRules).length > 0) {
            agentConfig.business_rules = businessRules;
        }
        
        // Validation
        if (!agentName || !voiceTenantId) {
            this.showToast('Please fill in Agent Name and Tenant', 'error');
            return;
        }

        try {
            this.saveAgentBtn.disabled = true;
            this.saveAgentBtn.innerHTML = '<span class="btn-icon">⏳</span> Saving...';

            let response;
            if (this.currentAgent) {
                // Update existing agent - use UpdateVoiceAgentRequest format
                const updateData = {
                    agent_name: agentName,
                    instruction: instruction || null,
                    agent_config: Object.keys(agentConfig).length > 0 ? agentConfig : null,
                    disabled: disabled
                };
                response = await apiClient.put(`/api/agents/${this.currentAgent.id}`, updateData);
            } else {
                // Create new agent - use CreateVoiceAgentRequest format
                const createData = {
                    voice_tenant_id: voiceTenantId,
                    agent_name: agentName,
                    instruction: instruction || null,
                    agent_config: Object.keys(agentConfig).length > 0 ? agentConfig : null
                };
                response = await apiClient.post('/api/agents', createData);
            }

            if (!response.ok) {
                const errorData = await response.text();
                throw new Error(errorData || `HTTP error! status: ${response.status}`);
            }

            const savedAgent = await response.json();
            
            if (this.currentAgent) {
                this.showToast('Agent updated successfully', 'success');
            } else {
                this.showToast('Agent created successfully', 'success');
            }

            this.closeAgentModal();
            this.loadData();

        } catch (error) {
            console.error('Error saving agent:', error);
            this.showToast(`Failed to save agent: ${error.message}`, 'error');
        } finally {
            this.saveAgentBtn.disabled = false;
            this.saveAgentBtn.innerHTML = this.currentAgent ? 
                '<span class="btn-icon">💾</span> Update Agent' : 
                '<span class="btn-icon">💾</span> Create Agent';
        }
    }

    editAgent(agentId) {
        const agent = this.agents.find(a => a.id === agentId);
        if (agent) {
            this.openAgentModal(agent);
        }
    }

    viewAgent(agentId) {
        const agent = this.agents.find(a => a.id === agentId);
        if (agent) {
            this.showAgentDetails(agent);
        }
    }

    showAgentDetails(agent) {
        const detailsHtml = `
            <div style="padding: 20px; max-width: 600px; max-height: 80vh; overflow-y: auto;">
                <h3 style="margin-bottom: 20px; color: #374151;">Agent Details</h3>
                
                <div style="display: grid; gap: 12px;">
                    <div style="display: flex; justify-content: space-between; padding: 8px 0; border-bottom: 1px solid #e5e7eb;">
                        <strong>Name:</strong>
                        <span>${this.escapeHtml(agent.name)}</span>
                    </div>
                    <div style="display: flex; flex-direction: column; padding: 8px 0; border-bottom: 1px solid #e5e7eb;">
                        <strong style="margin-bottom: 4px;">Agent ID:</strong>
                        <code style="background: #f3f4f6; padding: 4px 8px; border-radius: 4px; font-size: 0.75rem; word-break: break-all;">
                            ${this.escapeHtml(agent.id)}
                        </code>
                    </div>
                    <div style="display: flex; justify-content: space-between; padding: 8px 0; border-bottom: 1px solid #e5e7eb;">
                        <strong>Type:</strong>
                        <span class="type-badge type-${agent.type}">${this.formatAgentType(agent.type)}</span>
                    </div>
                    <div style="display: flex; justify-content: space-between; padding: 8px 0; border-bottom: 1px solid #e5e7eb;">
                        <strong>Tenant:</strong>
                        <span>${this.escapeHtml(agent.tenant_name || 'Unknown')}</span>
                    </div>
                    <div style="display: flex; flex-direction: column; padding: 8px 0; border-bottom: 1px solid #e5e7eb;">
                        <strong style="margin-bottom: 4px;">Tenant Business ID:</strong>
                        <code style="background: #dbeafe; padding: 4px 8px; border-radius: 4px; font-size: 0.85rem; color: #1e40af; font-weight: 500;">
                            ${this.escapeHtml(agent.tenant_business_id || agent.voice_tenant_id || 'N/A')}
                        </code>
                    </div>
                    <div style="display: flex; flex-direction: column; padding: 8px 0; border-bottom: 1px solid #e5e7eb;">
                        <strong style="margin-bottom: 4px;">Tenant UUID:</strong>
                        <code style="background: #f3f4f6; padding: 4px 8px; border-radius: 4px; font-size: 0.75rem; word-break: break-all;">
                            ${this.escapeHtml(agent.tenant_uuid || 'N/A')}
                        </code>
                    </div>
                    <div style="display: flex; justify-content: space-between; padding: 8px 0; border-bottom: 1px solid #e5e7eb;">
                        <strong>Voice:</strong>
                        <span>${this.escapeHtml(agent.voice || 'alloy')} (${agent.speed || 1.0}x)</span>
                    </div>
                    <div style="display: flex; justify-content: space-between; padding: 8px 0; border-bottom: 1px solid #e5e7eb;">
                        <strong>Language:</strong>
                        <span>${this.escapeHtml(agent.language || 'en')}</span>
                    </div>
                    ${agent.business_number ? `
                    <div style="display: flex; justify-content: space-between; padding: 8px 0; border-bottom: 1px solid #e5e7eb;">
                        <strong>Business Number:</strong>
                        <code style="background: #f3f4f6; padding: 2px 6px; border-radius: 4px;">${this.escapeHtml(agent.business_number)}</code>
                    </div>
                    ` : ''}
                    <div style="display: flex; justify-content: space-between; padding: 8px 0; border-bottom: 1px solid #e5e7eb;">
                        <strong>Status:</strong>
                        <span class="status-badge ${agent.is_enabled ? 'status-active' : 'status-disabled'}">
                            ${agent.is_enabled ? 'Active' : 'Disabled'}
                        </span>
                    </div>
                    <div style="display: flex; justify-content: space-between; padding: 8px 0; border-bottom: 1px solid #e5e7eb;">
                        <strong>Created:</strong>
                        <span>${this.formatDate(agent.created_at)}</span>
                    </div>
                    <div style="display: flex; justify-content: space-between; padding: 8px 0; border-bottom: 1px solid #e5e7eb;">
                        <strong>Updated:</strong>
                        <span>${this.formatDate(agent.updated_at)}</span>
                    </div>
                    ${agent.description ? `
                    <div style="padding: 8px 0; border-bottom: 1px solid #e5e7eb;">
                        <strong>Description:</strong>
                        <p style="margin-top: 8px; color: #6b7280;">${this.escapeHtml(agent.description)}</p>
                    </div>
                    ` : ''}
                    <div style="padding: 8px 0;">
                        <strong>System Prompt:</strong>
                        <div style="margin-top: 8px; padding: 12px; background: #f9fafb; border-radius: 8px; font-family: monospace; font-size: 0.85rem; white-space: pre-wrap; max-height: 200px; overflow-y: auto;">
                            ${this.escapeHtml(agent.system_prompt || 'No prompt configured')}
                        </div>
                    </div>
                </div>
            </div>
        `;

        this.showToast(detailsHtml, 'info', 15000);
    }

    async showJWTForAgent(agentId) {
        const agent = this.agents.find(a => a.id === agentId);
        if (!agent) {
            this.showToast('Agent not found', 'error');
            return;
        }

        try {
            const response = await apiClient.get(`/api/agents/${agentId}/jwt`);
            
            if (!response.ok) {
                throw new Error(`HTTP error! status: ${response.status}`);
            }

            const data = await response.json();
            
            // Display JWT token with copy functionality
            const jwtHtml = `
                <div style="padding: 20px; max-width: 700px;">
                    <h3 style="margin-bottom: 20px; color: #374151; display: flex; align-items: center; gap: 8px;">
                        🔑 JWT Token for ${this.escapeHtml(agent.name)}
                    </h3>
                    
                    <div style="display: grid; gap: 16px;">
                        <div style="padding: 12px; background: #f9fafb; border-radius: 8px; border: 2px solid #e5e7eb;">
                            <strong style="color: #374151; display: block; margin-bottom: 8px;">Tenant ID:</strong>
                            <code style="background: #dbeafe; padding: 6px 12px; border-radius: 4px; font-size: 0.9rem; color: #1e40af; font-weight: 500; display: block;">
                                ${this.escapeHtml(data.tenant_id)}
                            </code>
                        </div>
                        
                        <div style="padding: 12px; background: #f9fafb; border-radius: 8px; border: 2px solid #e5e7eb;">
                            <strong style="color: #374151; display: block; margin-bottom: 8px;">Agent ID:</strong>
                            <code style="background: #f3f4f6; padding: 6px 12px; border-radius: 4px; font-size: 0.85rem; display: block; word-break: break-all;">
                                ${this.escapeHtml(data.agent_id)}
                            </code>
                        </div>
                        
                        <div style="padding: 12px; background: #fffbeb; border-radius: 8px; border: 2px solid #fbbf24;">
                            <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 8px;">
                                <strong style="color: #92400e;">JWT Token:</strong>
                                <button 
                                    onclick="navigator.clipboard.writeText('${this.escapeHtml(data.jwt)}').then(() => alert('JWT copied to clipboard!'))" 
                                    style="background: #fbbf24; color: #92400e; border: none; padding: 6px 12px; border-radius: 4px; cursor: pointer; font-size: 0.85rem; font-weight: 600;">
                                    📋 Copy
                                </button>
                            </div>
                            <div style="background: white; padding: 12px; border-radius: 4px; font-family: monospace; font-size: 0.8rem; word-break: break-all; max-height: 150px; overflow-y: auto; border: 1px solid #fbbf24;">
                                ${this.escapeHtml(data.jwt)}
                            </div>
                        </div>
                        
                        <div style="padding: 12px; background: #eff6ff; border-radius: 8px; border: 1px solid #3b82f6;">
                            <strong style="color: #1e40af; display: block; margin-bottom: 8px;">💡 Usage:</strong>
                            <p style="margin: 0; color: #1e3a8a; font-size: 0.9rem; line-height: 1.5;">
                                Use this JWT token as the <code style="background: white; padding: 2px 6px; border-radius: 3px;">apiKey</code> field in webhook requests. 
                                The system will automatically decode it to extract <code style="background: white; padding: 2px 6px; border-radius: 3px;">tenantId</code> and <code style="background: white; padding: 2px 6px; border-radius: 3px;">agentId</code>.
                            </p>
                        </div>
                    </div>
                </div>
            `;

            this.showToast(jwtHtml, 'info', 20000);
            
        } catch (error) {
            console.error('Error generating JWT:', error);
            this.showToast(`Failed to generate JWT: ${error.message}`, 'error');
        }
    }

    async publishAgent(agentId) {
        const agent = this.agents.find(a => a.id === agentId);
        if (!agent) return;

        if (!confirm(`Are you sure you want to publish agent "${agent.name}"? This will update the live configuration.`)) {
            return;
        }

        try {
            // Use POST request with body {"agent_id": "..."}
            const response = await apiClient.post('/api/agents/publish', {
                agent_id: agentId
            });

            if (!response.ok) {
                const errorData = await response.text();
                throw new Error(errorData || `HTTP error! status: ${response.status}`);
            }

            this.showToast('Agent published successfully', 'success');
            this.loadData(); // Reload to update UI if needed (e.g. status)

        } catch (error) {
            console.error('Error publishing agent:', error);
            this.showToast(`Failed to publish agent: ${error.message}`, 'error');
        }
    }

    confirmDeleteAgent(agentId) {
        const agent = this.agents.find(a => a.id === agentId);
        if (agent) {
            this.currentAgent = agent;
            this.deleteAgentNameEl.textContent = agent.name;
            this.deleteModal.classList.add('active');
        }
    }

    closeDeleteModal() {
        this.deleteModal.classList.remove('active');
        this.currentAgent = null;
    }

    async deleteAgent() {
        if (!this.currentAgent) return;

        try {
            this.confirmDeleteBtn.disabled = true;
            this.confirmDeleteBtn.innerHTML = '<span class="btn-icon">⏳</span> Deleting...';

            const response = await apiClient.delete(`/api/agents/${this.currentAgent.id}`);

            if (!response.ok) {
                const errorData = await response.text();
                throw new Error(errorData || `HTTP error! status: ${response.status}`);
            }

            this.showToast('Agent deleted successfully', 'success');
            this.closeDeleteModal();
            this.loadData();

        } catch (error) {
            console.error('Error deleting agent:', error);
            this.showToast(`Failed to delete agent: ${error.message}`, 'error');
        } finally {
            this.confirmDeleteBtn.disabled = false;
            this.confirmDeleteBtn.innerHTML = '<span class="btn-icon">🗑️</span> Delete Agent';
        }
    }

    exportAgents() {
        if (this.filteredAgents.length === 0) {
            this.showToast('No agents to export', 'warning');
            return;
        }

        const csvData = this.convertToCSV(this.filteredAgents);
        const blob = new Blob([csvData], { type: 'text/csv' });
        const url = window.URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url;
        a.download = `agents_${new Date().toISOString().split('T')[0]}.csv`;
        document.body.appendChild(a);
        a.click();
        document.body.removeChild(a);
        window.URL.revokeObjectURL(url);

        this.showToast('Agents exported successfully', 'success');
    }

    convertToCSV(data) {
        const headers = ['Name', 'Agent ID', 'Type', 'Tenant', 'Tenant Business ID', 'Tenant UUID', 'Business Number', 'Voice', 'Speed', 'Language', 'Status', 'Description', 'Created', 'Updated'];
        const rows = data.map(agent => [
            agent.name,
            agent.id,
            this.formatAgentType(agent.type),
            agent.tenant_name || 'Unknown',
            agent.tenant_business_id || agent.voice_tenant_id || 'N/A',
            agent.tenant_uuid || 'N/A',
            agent.business_number || '',
            agent.voice || 'alloy',
            agent.speed || 1.0,
            agent.language || 'en',
            agent.is_enabled ? 'Active' : 'Disabled',
            agent.description || '',
            this.formatDate(agent.created_at),
            this.formatDate(agent.updated_at)
        ]);

        const csvContent = [headers, ...rows]
            .map(row => row.map(field => `"${String(field).replace(/"/g, '""')}"`).join(','))
            .join('\n');

        return csvContent;
    }

    formatAgentType(type) {
        const typeMap = {
            wati: 'WATI',
            automotive: 'Automotive',
            financial: 'Financial',
            restaurant: 'Restaurant',
            travel: 'Travel',
            custom: 'Custom'
        };
        return typeMap[type] || type;
    }

    setLoading(loading) {
        this.isLoading = loading;
        this.loadingState.style.display = loading ? 'block' : 'none';
        
        if (!loading) {
            this.renderAgents();
        }
    }

    closeModals() {
        this.closeAgentModal();
        this.closePromptPreviewModal();
        this.closeDeleteModal();
    }

    updateAccentOptions() {
        if (!this.defaultAccentField || !this.languageField) return;
        
        const language = this.languageField.value || 'en';
        const currentValue = this.defaultAccentField.value;
        
        // Clear existing options except the first one
        this.defaultAccentField.innerHTML = '<option value="">-- No Default Accent --</option>';
        
        // Define accent options by language
        const accentOptions = {
            'en': [
                { value: 'india', label: '🇮🇳 Indian English' },
                { value: 'singapore', label: '🇸🇬 Singaporean English' },
                { value: 'malaysia', label: '🇲🇾 Malaysian English' },
                { value: 'philippines', label: '🇵🇭 Filipino English' },
                { value: 'hongkong', label: '🇭🇰 Hong Kong English' },
                { value: 'us', label: '🇺🇸 American English' },
                { value: 'uk', label: '🇬🇧 British English (RP)' },
                { value: 'australia', label: '🇦🇺 Australian English' },
                { value: 'newzealand', label: '🇳🇿 New Zealand English' },
                { value: 'canada', label: '🇨🇦 Canadian English' },
                { value: 'ireland', label: '🇮🇪 Irish English' },
                { value: 'scotland', label: '🏴󠁧󠁢󠁳󠁣󠁴󠁿 Scottish English' },
                { value: 'southafrica', label: '🇿🇦 South African English' },
                { value: 'nigeria', label: '🇳🇬 Nigerian English' },
                { value: 'kenya', label: '🇰🇪 Kenyan English' },
                { value: 'southern', label: '🇺🇸 Southern American' },
                { value: 'newyork', label: '🗽 New York English' },
                { value: 'boston', label: '🇺🇸 Boston English' },
                { value: 'london', label: '🇬🇧 London/Cockney English' },
                { value: 'liverpool', label: '🇬🇧 Liverpool/Scouse English' },
                { value: 'manchester', label: '🇬🇧 Manchester English' }
            ],
            'zh': [
                { value: 'mainland', label: '🇨🇳 Mainland Chinese (普通话)' },
                { value: 'taiwan', label: '🇹🇼 Taiwanese Chinese (台湾国语)' },
                { value: 'singapore', label: '🇸🇬 Singaporean Chinese (新加坡华语)' }
            ],
            'yue': [
                { value: 'hongkong', label: '🇭🇰 Hong Kong Cantonese (香港粤语)' },
                { value: 'guangdong', label: '🇨🇳 Guangdong Cantonese (广东粤语)' }
            ],
            'es': [
                { value: 'spain', label: '🇪🇸 Spanish (Castilian)' },
                { value: 'mexico', label: '🇲🇽 Mexican Spanish' },
                { value: 'latin', label: '🌎 Latin American Spanish' }
            ]
        };
        
        // Get accents for the selected language
        const accents = accentOptions[language] || [];
        
        // Add options
        accents.forEach(accent => {
            const option = document.createElement('option');
            option.value = accent.value;
            option.textContent = accent.label;
            this.defaultAccentField.appendChild(option);
        });
        
        // Restore previous value if it's still valid
        if (currentValue && accents.find(a => a.value === currentValue)) {
            this.defaultAccentField.value = currentValue;
        } else {
            this.defaultAccentField.value = '';
        }
    }

    formatDate(dateString) {
        if (!dateString) return '-';
        const date = new Date(dateString);
        return date.toLocaleDateString() + ' ' + date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
    }

    escapeHtml(text) {
        const div = document.createElement('div');
        div.textContent = text;
        return div.innerHTML;
    }

    showToast(message, type = 'info', duration = 5000) {
        const toast = document.createElement('div');
        toast.className = `toast toast-${type}`;
        
        const icon = {
            success: '✅',
            error: '❌',
            warning: '⚠️',
            info: 'ℹ️'
        }[type] || 'ℹ️';

        toast.innerHTML = `
            <div class="toast-icon">${icon}</div>
            <div class="toast-content">
                <div class="toast-message">${message}</div>
            </div>
            <button class="toast-close" title="Close (or click anywhere)">&times;</button>
        `;

        this.toastContainer.appendChild(toast);

        // Function to remove toast
        const removeToast = () => {
            if (toast.parentNode) {
                toast.classList.add('toast-removing');
                setTimeout(() => {
                    if (toast.parentNode) {
                        toast.parentNode.removeChild(toast);
                    }
                }, 300); // Match CSS transition duration
            }
        };

        // Auto remove after duration
        const autoRemoveTimer = setTimeout(removeToast, duration);

        // Click to dismiss (entire toast)
        toast.addEventListener('click', (e) => {
            // If clicking the close button, let its handler deal with it
            if (e.target.classList.contains('toast-close')) {
                return;
            }
            clearTimeout(autoRemoveTimer);
            removeToast();
        });

        // Close button click handler
        const closeBtn = toast.querySelector('.toast-close');
        closeBtn.addEventListener('click', (e) => {
            e.stopPropagation(); // Prevent toast click event
            clearTimeout(autoRemoveTimer);
            removeToast();
        });
    }
}

// Global functions for button onclick handlers
window.openAgentModal = () => agentManager.openAgentModal();
window.closeAgentModal = () => agentManager.closeAgentModal();
window.closePromptPreviewModal = () => agentManager.closePromptPreviewModal();
window.closeDeleteModal = () => agentManager.closeDeleteModal();

// ===== Brandkit Generator Functions =====
let generatedBrandkitConfig = null;

window.openBrandkitGeneratorModal = () => {
    const modal = document.getElementById('brandkitGeneratorModal');
    modal.classList.add('active');
    
    // Reset form
    document.getElementById('textAgentId').value = '';
    document.getElementById('brandkitGeneratorResult').style.display = 'none';
    document.getElementById('brandkitGeneratorError').style.display = 'none';
    document.getElementById('brandkitGeneratorLoading').style.display = 'none';
    document.getElementById('applyBrandkitConfigBtn').style.display = 'none';
    generatedBrandkitConfig = null;
};

window.closeBrandkitGeneratorModal = () => {
    const modal = document.getElementById('brandkitGeneratorModal');
    modal.classList.remove('active');
};

window.generateConfigFromBrandkit = async () => {
    const textAgentId = document.getElementById('textAgentId').value.trim();
    
    if (!textAgentId) {
        agentManager.showToast('请输入 Text Agent ID', 'error');
        return;
    }
    
    // Hide previous results/errors
    document.getElementById('brandkitGeneratorResult').style.display = 'none';
    document.getElementById('brandkitGeneratorError').style.display = 'none';
    document.getElementById('applyBrandkitConfigBtn').style.display = 'none';
    
    // Show loading
    document.getElementById('brandkitGeneratorLoading').style.display = 'block';
    document.getElementById('generateFromBrandkitBtn').disabled = true;
    
    try {
        const response = await apiClient.post('/api/agents/generate-from-brandkit', {
            text_agent_id: textAgentId
        });
        
        if (!response.ok) {
            const errorData = await response.text();
            throw new Error(errorData || `HTTP error! status: ${response.status}`);
        }
        
        const config = await response.json();
        generatedBrandkitConfig = config;
        
        // Display result
        displayBrandkitConfig(config);
        document.getElementById('brandkitGeneratorResult').style.display = 'block';
        document.getElementById('applyBrandkitConfigBtn').style.display = 'inline-block';
        
        agentManager.showToast('✅ 配置生成成功！', 'success');
        
    } catch (error) {
        console.error('Brandkit generation error:', error);
        document.getElementById('brandkitGeneratorError').style.display = 'block';
        document.getElementById('brandkitGeneratorErrorMessage').textContent = error.message;
        agentManager.showToast('生成配置失败', 'error');
    } finally {
        document.getElementById('brandkitGeneratorLoading').style.display = 'none';
        document.getElementById('generateFromBrandkitBtn').disabled = false;
    }
};

function displayBrandkitConfig(config) {
    const resultContent = document.getElementById('brandkitGeneratorResultContent');
    
    let html = '<div style="background: #f5f5f5; padding: 16px; border-radius: 8px; max-height: 400px; overflow-y: auto;">';
    
    if (config.persona) {
        html += `
            <div style="margin-bottom: 16px;">
                <strong style="color: #667eea;">Persona:</strong>
                <div style="background: white; padding: 10px; margin-top: 4px; border-radius: 4px;">${config.persona}</div>
            </div>
        `;
    }
    
    if (config.tone) {
        html += `
            <div style="margin-bottom: 16px;">
                <strong style="color: #667eea;">Tone:</strong>
                <div style="background: white; padding: 10px; margin-top: 4px; border-radius: 4px;">${config.tone}</div>
            </div>
        `;
    }
    
    if (config.language) {
        html += `
            <div style="margin-bottom: 16px;">
                <strong style="color: #667eea;">Language:</strong>
                <div style="background: white; padding: 10px; margin-top: 4px; border-radius: 4px;">${config.language}</div>
            </div>
        `;
    }
    
    if (config.default_accent) {
        html += `
            <div style="margin-bottom: 16px;">
                <strong style="color: #667eea;">Default Accent:</strong>
                <div style="background: white; padding: 10px; margin-top: 4px; border-radius: 4px;">${config.default_accent}</div>
            </div>
        `;
    }
    
    if (config.voice) {
        html += `
            <div style="margin-bottom: 16px;">
                <strong style="color: #667eea;">Voice:</strong>
                <div style="background: white; padding: 10px; margin-top: 4px; border-radius: 4px;">${config.voice}</div>
            </div>
        `;
    }
    
    if (config.speed) {
        html += `
            <div style="margin-bottom: 16px;">
                <strong style="color: #667eea;">Speed:</strong>
                <div style="background: white; padding: 10px; margin-top: 4px; border-radius: 4px;">${config.speed}</div>
            </div>
        `;
    }
    
    if (config.services && config.services.length > 0) {
        html += `
            <div style="margin-bottom: 16px;">
                <strong style="color: #667eea;">Services:</strong>
                <div style="background: white; padding: 10px; margin-top: 4px; border-radius: 4px;">${config.services.join(', ')}</div>
            </div>
        `;
    }
    
    if (config.expertise && config.expertise.length > 0) {
        html += `
            <div style="margin-bottom: 16px;">
                <strong style="color: #667eea;">Expertise:</strong>
                <div style="background: white; padding: 10px; margin-top: 4px; border-radius: 4px;">${config.expertise.join(', ')}</div>
            </div>
        `;
    }
    
    if (config.greeting_template) {
        html += `
            <div style="margin-bottom: 16px;">
                <strong style="color: #667eea;">Greeting Template:</strong>
                <div style="background: white; padding: 10px; margin-top: 4px; border-radius: 4px; white-space: pre-wrap;">${config.greeting_template}</div>
            </div>
        `;
    }
    
    if (config.realtime_template) {
        html += `
            <div style="margin-bottom: 16px;">
                <strong style="color: #667eea;">Realtime Template:</strong>
                <div style="background: white; padding: 10px; margin-top: 4px; border-radius: 4px; white-space: pre-wrap; max-height: 200px; overflow-y: auto;">${config.realtime_template}</div>
            </div>
        `;
    }
    
    if (config.system_instructions) {
        html += `
            <div style="margin-bottom: 16px;">
                <strong style="color: #667eea;">System Instructions:</strong>
                <div style="background: white; padding: 10px; margin-top: 4px; border-radius: 4px; white-space: pre-wrap;">${config.system_instructions}</div>
            </div>
        `;
    }
    
    html += '</div>';
    resultContent.innerHTML = html;
}

window.applyBrandkitConfig = () => {
    if (!generatedBrandkitConfig) {
        agentManager.showToast('没有可应用的配置', 'error');
        return;
    }
    
    const config = generatedBrandkitConfig;
    let appliedCount = 0;
    let missingFields = [];
    
    // Apply to form fields
    if (config.persona) {
        const personaField = document.getElementById('persona');
        if (personaField) {
            personaField.value = config.persona;
            appliedCount++;
        } else {
            missingFields.push('persona');
        }
    }
    
    if (config.tone) {
        const toneSelect = document.getElementById('toneSelect');
        const toneInput = document.getElementById('tone');
        
        if (toneSelect && toneInput) {
            // Check if tone matches predefined options
            const toneOption = Array.from(toneSelect.options).find(opt => opt.value === config.tone);
            if (toneOption) {
                toneSelect.value = config.tone;
                toneInput.style.display = 'none';
                toneInput.value = '';
            } else {
                toneSelect.value = '__custom__';
                toneInput.style.display = 'block';
                toneInput.value = config.tone;
            }
            appliedCount++;
        } else {
            missingFields.push('tone');
        }
    }
    
    if (config.language) {
        const langSelect = document.getElementById('language');
        if (langSelect) {
            langSelect.value = config.language;
            // Trigger accent options update
            if (agentManager && typeof agentManager.updateAccentOptions === 'function') {
                agentManager.updateAccentOptions();
            }
            appliedCount++;
        } else {
            missingFields.push('language');
        }
    }
    
    if (config.default_accent) {
        const accentSelect = document.getElementById('defaultAccent');
        if (accentSelect) {
            accentSelect.value = config.default_accent;
            appliedCount++;
        } else {
            missingFields.push('default_accent');
        }
    }
    
    if (config.voice) {
        const voiceSelect = document.getElementById('voice');
        if (voiceSelect) {
            voiceSelect.value = config.voice;
            appliedCount++;
        } else {
            missingFields.push('voice');
        }
    }
    
    if (config.speed) {
        const speedSlider = document.getElementById('speedSlider');
        const speedValue = document.getElementById('speedValue');
        if (speedSlider && speedValue) {
            speedSlider.value = config.speed;
            speedValue.textContent = config.speed;
            appliedCount++;
        } else {
            missingFields.push('speed');
        }
    }
    
    if (config.services && config.services.length > 0) {
        const servicesField = document.getElementById('services');
        if (servicesField) {
            servicesField.value = config.services.join(', ');
            appliedCount++;
        } else {
            missingFields.push('services');
        }
    }
    
    if (config.expertise && config.expertise.length > 0) {
        const expertiseField = document.getElementById('expertise');
        if (expertiseField) {
            expertiseField.value = config.expertise.join(', ');
            appliedCount++;
        } else {
            missingFields.push('expertise');
        }
    }
    
    if (config.greeting_template) {
        const greetingField = document.getElementById('greetingTemplate');
        if (greetingField) {
            greetingField.value = decodeTextNewlines(config.greeting_template);
            appliedCount++;
        } else {
            missingFields.push('greetingTemplate');
        }
    }
    
    if (config.realtime_template) {
        const realtimeField = document.getElementById('realtimeTemplate');
        if (realtimeField) {
            realtimeField.value = decodeTextNewlines(config.realtime_template);
            appliedCount++;
        } else {
            missingFields.push('realtimeTemplate');
        }
    }
    
    if (config.system_instructions) {
        const instructionsField = document.getElementById('systemInstructions');
        if (instructionsField) {
            instructionsField.value = config.system_instructions;
            appliedCount++;
        } else {
            missingFields.push('systemInstructions');
        }
    }
    
    // Log missing fields for debugging
    if (missingFields.length > 0) {
        console.warn('⚠️ The following fields were not found in the form:', missingFields);
    }
    
    // Close modal
    closeBrandkitGeneratorModal();
    
    // Switch to Config tab to show the applied values
    const configTab = document.getElementById('tabConfig');
    const basicTab = document.getElementById('tabBasic');
    const configTabBtn = document.querySelector('[data-tab="config"]');
    const basicTabBtn = document.querySelector('[data-tab="basic"]');
    
    if (configTab && basicTab && configTabBtn && basicTabBtn) {
        basicTab.style.display = 'none';
        configTab.style.display = 'block';
        basicTabBtn.classList.remove('active');
        configTabBtn.classList.add('active');
    }
    
    // Show success message with details
    if (appliedCount > 0) {
        const message = missingFields.length > 0 
            ? `✅ 已应用 ${appliedCount} 个字段 (${missingFields.length} 个字段未找到)`
            : `✅ 配置已成功应用！(${appliedCount} 个字段)`;
        agentManager.showToast(message, 'success');
    } else {
        agentManager.showToast('⚠️ 未能应用任何字段，请检查表单', 'warning');
    }
};

// Initialize when DOM is loaded
let agentManager;
document.addEventListener('DOMContentLoaded', () => {
    agentManager = new AgentManager();
});
