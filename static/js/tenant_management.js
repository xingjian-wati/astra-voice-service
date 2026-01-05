// Tenant Management JavaScript
class TenantManager {
    constructor() {
        this.tenants = [];
        this.filteredTenants = [];
        this.currentTenant = null;
        this.isLoading = false;
        
        this.initializeElements();
        this.attachEventListeners();
        this.loadTenants();
    }

    initializeElements() {
        // Main elements
        this.addTenantBtn = document.getElementById('addTenantBtn');
        this.refreshBtn = document.getElementById('refreshBtn');
        this.searchInput = document.getElementById('searchInput');
        this.includeDisabledFilter = document.getElementById('includeDisabledFilter');
        this.sortSelect = document.getElementById('sortSelect');
        this.exportBtn = document.getElementById('exportBtn');

        // Statistics elements
        this.totalTenantsEl = document.getElementById('totalTenants');
        this.activeTenantsEl = document.getElementById('activeTenants');
        this.disabledTenantsEl = document.getElementById('disabledTenants');
        this.totalAgentsEl = document.getElementById('totalAgents');

        // Table elements
        this.tenantsTableBody = document.getElementById('tenantsTableBody');
        this.loadingState = document.getElementById('loadingState');
        this.emptyState = document.getElementById('emptyState');

        // Modal elements
        this.tenantModal = document.getElementById('tenantModal');
        this.tenantForm = document.getElementById('tenantForm');
        this.modalTitle = document.getElementById('modalTitle');
        this.saveTenantBtn = document.getElementById('saveTenantBtn');

        // Form elements
        this.tenantIdField = document.getElementById('tenantId');
        this.tenantNameField = document.getElementById('tenantName');
        this.tenantIdInputField = document.getElementById('tenantIdInput');
        this.astraKeyField = document.getElementById('astraKey');
        this.descriptionField = document.getElementById('description');
        this.isEnabledField = document.getElementById('isEnabled');

        // Delete modal elements
        this.deleteModal = document.getElementById('deleteModal');
        this.deleteTenantNameEl = document.getElementById('deleteTenantName');
        this.confirmDeleteBtn = document.getElementById('confirmDeleteBtn');

        // Toast container
        this.toastContainer = document.getElementById('toastContainer');
    }

    attachEventListeners() {
        // Main actions
        this.addTenantBtn.addEventListener('click', () => this.openTenantModal());
        this.refreshBtn.addEventListener('click', () => this.loadTenants());
        this.exportBtn.addEventListener('click', () => this.exportTenants());

        // Filters and search
        this.searchInput.addEventListener('input', () => this.filterTenants());
        this.includeDisabledFilter.addEventListener('change', () => this.filterTenants());
        this.sortSelect.addEventListener('change', () => this.sortTenants());

        // Form submission
        this.tenantForm.addEventListener('submit', (e) => this.handleFormSubmit(e));

        // Delete confirmation
        this.confirmDeleteBtn.addEventListener('click', () => this.deleteTenant());

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

    async loadTenants() {
        this.setLoading(true);
        
        try {
            const includeDisabled = this.includeDisabledFilter.checked;
            const response = await apiClient.get(`/api/tenants?include_disabled=${includeDisabled}`);
            
            if (!response.ok) {
                throw new Error(`HTTP error! status: ${response.status}`);
            }
            
            const rawTenants = await response.json();
            
            // Transform backend data to match frontend expectations
            this.tenants = rawTenants.map(tenant => ({
                ...tenant,
                name: tenant.tenant_name || tenant.name || '',
                is_enabled: tenant.disabled !== undefined ? !tenant.disabled : (tenant.is_enabled !== undefined ? tenant.is_enabled : true),
                agent_count: tenant.agent_count || 0,
                description: this.extractDescription(tenant)
            }));
            
            this.filterTenants();
            this.updateStatistics();
            this.showToast('Tenants loaded successfully', 'success');
            
        } catch (error) {
            console.error('Error loading tenants:', error);
            this.showToast('Failed to load tenants', 'error');
            this.tenants = [];
            this.filteredTenants = [];
        } finally {
            this.setLoading(false);
        }
    }

    extractDescription(tenant) {
        // Try to extract description from custom_config
        if (tenant.description) return tenant.description;
        if (tenant.custom_config && typeof tenant.custom_config === 'object') {
            return tenant.custom_config.description || '';
        }
        return '';
    }

    filterTenants() {
        const searchTerm = this.searchInput.value.toLowerCase();
        const includeDisabled = this.includeDisabledFilter.checked;

        this.filteredTenants = this.tenants.filter(tenant => {
            // Search filter
            const matchesSearch = !searchTerm || 
                tenant.name.toLowerCase().includes(searchTerm) ||
                tenant.tenant_id.toLowerCase().includes(searchTerm) ||
                tenant.astra_key.toLowerCase().includes(searchTerm);

            // Status filter
            const matchesStatus = includeDisabled || tenant.is_enabled;

            return matchesSearch && matchesStatus;
        });

        this.sortTenants();
    }

    sortTenants() {
        const sortBy = this.sortSelect.value;
        
        this.filteredTenants.sort((a, b) => {
            switch (sortBy) {
                case 'name':
                    return a.name.localeCompare(b.name);
                case 'created_at':
                    return new Date(b.created_at) - new Date(a.created_at);
                case 'updated_at':
                    return new Date(b.updated_at) - new Date(a.updated_at);
                default:
                    return a.name.localeCompare(b.name);
            }
        });

        this.renderTenants();
    }

    renderTenants() {
        if (this.filteredTenants.length === 0) {
            this.tenantsTableBody.innerHTML = '';
            this.emptyState.style.display = 'block';
            return;
        }

        this.emptyState.style.display = 'none';
        
        this.tenantsTableBody.innerHTML = this.filteredTenants.map(tenant => `
            <tr>
                <td>
                    <div style="font-weight: 600; color: #374151;">${this.escapeHtml(tenant.name)}</div>
                    ${tenant.description ? `<div style="font-size: 0.8rem; color: #6b7280; margin-top: 2px;">${this.escapeHtml(tenant.description)}</div>` : ''}
                </td>
                <td>
                    <code style="background: #f3f4f6; padding: 2px 6px; border-radius: 4px; font-size: 0.85rem;">
                        ${this.escapeHtml(tenant.tenant_id)}
                    </code>
                </td>
                <td>
                    <code style="background: #f3f4f6; padding: 2px 6px; border-radius: 4px; font-size: 0.85rem;">
                        ${this.escapeHtml(tenant.astra_key.substring(0, 20))}...
                    </code>
                </td>
                <td>
                    <span class="status-badge ${tenant.is_enabled ? 'status-active' : 'status-disabled'}">
                        ${tenant.is_enabled ? 'Active' : 'Disabled'}
                    </span>
                </td>
                <td>
                    <span style="font-weight: 600; color: #374151;">${tenant.agent_count || 0}</span>
                    <span style="font-size: 0.8rem; color: #6b7280;">agents</span>
                </td>
                <td>
                    <div style="font-size: 0.85rem; color: #374151;">
                        ${this.formatDate(tenant.created_at)}
                    </div>
                </td>
                <td>
                    <div class="action-buttons">
                        <button class="action-btn action-btn-view" onclick="tenantManager.viewTenant('${tenant.id}')" title="View Details">
                            👁️ View
                        </button>
                        <button class="action-btn action-btn-edit" onclick="tenantManager.editTenant('${tenant.id}')" title="Edit Tenant">
                            ✏️ Edit
                        </button>
                        <button class="action-btn action-btn-delete" onclick="tenantManager.confirmDeleteTenant('${tenant.id}')" title="Delete Tenant">
                            🗑️ Delete
                        </button>
                    </div>
                </td>
            </tr>
        `).join('');
    }

    updateStatistics() {
        const totalTenants = this.tenants.length;
        const activeTenants = this.tenants.filter(t => t.is_enabled).length;
        const disabledTenants = totalTenants - activeTenants;
        const totalAgents = this.tenants.reduce((sum, t) => sum + (t.agent_count || 0), 0);

        this.totalTenantsEl.textContent = totalTenants;
        this.activeTenantsEl.textContent = activeTenants;
        this.disabledTenantsEl.textContent = disabledTenants;
        this.totalAgentsEl.textContent = totalAgents;
    }

    openTenantModal(tenant = null) {
        this.currentTenant = tenant;
        
        if (tenant) {
            // Edit mode
            this.modalTitle.textContent = 'Edit Tenant';
            this.tenantIdField.value = tenant.id;
            this.tenantNameField.value = tenant.name || tenant.tenant_name || '';
            this.tenantIdInputField.value = tenant.tenant_id;
            this.astraKeyField.value = tenant.astra_key;
            this.descriptionField.value = tenant.description || this.extractDescription(tenant);
            this.isEnabledField.checked = tenant.is_enabled !== undefined ? tenant.is_enabled : !tenant.disabled;
            this.saveTenantBtn.innerHTML = '<span class="btn-icon">💾</span> Update Tenant';
        } else {
            // Create mode
            this.modalTitle.textContent = 'Add New Tenant';
            this.tenantForm.reset();
            this.tenantIdField.value = '';
            this.isEnabledField.checked = true;
            this.saveTenantBtn.innerHTML = '<span class="btn-icon">💾</span> Create Tenant';
        }
        
        this.tenantModal.classList.add('active');
        this.tenantNameField.focus();
    }

    closeTenantModal() {
        this.tenantModal.classList.remove('active');
        this.currentTenant = null;
        this.tenantForm.reset();
    }

    async handleFormSubmit(e) {
        e.preventDefault();
        
        const formData = new FormData(this.tenantForm);
        const isEnabled = formData.get('is_enabled') === 'on';
        const description = formData.get('description') || '';
        
        // Transform frontend data to match backend expectations
        const tenantData = {
            tenant_name: formData.get('name'),
            tenant_id: formData.get('tenant_id'),
            astra_key: formData.get('astra_key'),
            custom_config: description ? { description } : {},
            disabled: !isEnabled
        };

        // Validation
        if (!tenantData.tenant_name || !tenantData.tenant_id || !tenantData.astra_key) {
            this.showToast('Please fill in all required fields', 'error');
            return;
        }

        try {
            this.saveTenantBtn.disabled = true;
            this.saveTenantBtn.innerHTML = '<span class="btn-icon">⏳</span> Saving...';

            let response;
            if (this.currentTenant) {
                // Update existing tenant - use UpdateVoiceTenantRequest format
                const updateData = {
                    tenant_name: tenantData.tenant_name,
                    custom_config: tenantData.custom_config,
                    disabled: tenantData.disabled
                };
                response = await apiClient.put(`/api/tenants/${this.currentTenant.id}`, updateData);
            } else {
                // Create new tenant - use CreateVoiceTenantRequest format
                response = await apiClient.post('/api/tenants', tenantData);
            }

            if (!response.ok) {
                const errorData = await response.text();
                throw new Error(errorData || `HTTP error! status: ${response.status}`);
            }

            const savedTenant = await response.json();
            
            if (this.currentTenant) {
                this.showToast('Tenant updated successfully', 'success');
            } else {
                this.showToast('Tenant created successfully', 'success');
            }

            this.closeTenantModal();
            this.loadTenants();

        } catch (error) {
            console.error('Error saving tenant:', error);
            this.showToast(`Failed to save tenant: ${error.message}`, 'error');
        } finally {
            this.saveTenantBtn.disabled = false;
            this.saveTenantBtn.innerHTML = this.currentTenant ? 
                '<span class="btn-icon">💾</span> Update Tenant' : 
                '<span class="btn-icon">💾</span> Create Tenant';
        }
    }

    editTenant(tenantId) {
        const tenant = this.tenants.find(t => t.id === tenantId);
        if (tenant) {
            this.openTenantModal(tenant);
        }
    }

    viewTenant(tenantId) {
        const tenant = this.tenants.find(t => t.id === tenantId);
        if (tenant) {
            // Open a view-only modal or navigate to detail page
            this.showTenantDetails(tenant);
        }
    }

    showTenantDetails(tenant) {
        const detailsHtml = `
            <div style="padding: 20px; max-width: 500px;">
                <h3 style="margin-bottom: 20px; color: #374151;">Tenant Details</h3>
                
                <div style="display: grid; gap: 12px;">
                    <div style="display: flex; justify-content: space-between; padding: 8px 0; border-bottom: 1px solid #e5e7eb;">
                        <strong>Name:</strong>
                        <span>${this.escapeHtml(tenant.name)}</span>
                    </div>
                    <div style="display: flex; justify-content: space-between; padding: 8px 0; border-bottom: 1px solid #e5e7eb;">
                        <strong>Tenant ID:</strong>
                        <code style="background: #f3f4f6; padding: 2px 6px; border-radius: 4px;">${this.escapeHtml(tenant.tenant_id)}</code>
                    </div>
                    <div style="display: flex; justify-content: space-between; padding: 8px 0; border-bottom: 1px solid #e5e7eb;">
                        <strong>Astra Key:</strong>
                        <code style="background: #f3f4f6; padding: 2px 6px; border-radius: 4px; font-size: 0.8rem;">${this.escapeHtml(tenant.astra_key)}</code>
                    </div>
                    <div style="display: flex; justify-content: space-between; padding: 8px 0; border-bottom: 1px solid #e5e7eb;">
                        <strong>Status:</strong>
                        <span class="status-badge ${tenant.is_enabled ? 'status-active' : 'status-disabled'}">
                            ${tenant.is_enabled ? 'Active' : 'Disabled'}
                        </span>
                    </div>
                    <div style="display: flex; justify-content: space-between; padding: 8px 0; border-bottom: 1px solid #e5e7eb;">
                        <strong>Agents:</strong>
                        <span>${tenant.agent_count || 0}</span>
                    </div>
                    <div style="display: flex; justify-content: space-between; padding: 8px 0; border-bottom: 1px solid #e5e7eb;">
                        <strong>Created:</strong>
                        <span>${this.formatDate(tenant.created_at)}</span>
                    </div>
                    <div style="display: flex; justify-content: space-between; padding: 8px 0;">
                        <strong>Updated:</strong>
                        <span>${this.formatDate(tenant.updated_at)}</span>
                    </div>
                    ${tenant.description ? `
                    <div style="padding: 8px 0;">
                        <strong>Description:</strong>
                        <p style="margin-top: 8px; color: #6b7280;">${this.escapeHtml(tenant.description)}</p>
                    </div>
                    ` : ''}
                </div>
            </div>
        `;

        this.showToast(detailsHtml, 'info', 10000);
    }

    confirmDeleteTenant(tenantId) {
        const tenant = this.tenants.find(t => t.id === tenantId);
        if (tenant) {
            this.currentTenant = tenant;
            this.deleteTenantNameEl.textContent = tenant.name;
            this.deleteModal.classList.add('active');
        }
    }

    closeDeleteModal() {
        this.deleteModal.classList.remove('active');
        this.currentTenant = null;
    }

    async deleteTenant() {
        if (!this.currentTenant) return;

        try {
            this.confirmDeleteBtn.disabled = true;
            this.confirmDeleteBtn.innerHTML = '<span class="btn-icon">⏳</span> Deleting...';

            const response = await apiClient.delete(`/api/tenants/${this.currentTenant.id}`);

            if (!response.ok) {
                const errorData = await response.text();
                throw new Error(errorData || `HTTP error! status: ${response.status}`);
            }

            this.showToast('Tenant deleted successfully', 'success');
            this.closeDeleteModal();
            this.loadTenants();

        } catch (error) {
            console.error('Error deleting tenant:', error);
            this.showToast(`Failed to delete tenant: ${error.message}`, 'error');
        } finally {
            this.confirmDeleteBtn.disabled = false;
            this.confirmDeleteBtn.innerHTML = '<span class="btn-icon">🗑️</span> Delete Tenant';
        }
    }

    exportTenants() {
        if (this.filteredTenants.length === 0) {
            this.showToast('No tenants to export', 'warning');
            return;
        }

        const csvData = this.convertToCSV(this.filteredTenants);
        const blob = new Blob([csvData], { type: 'text/csv' });
        const url = window.URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url;
        a.download = `tenants_${new Date().toISOString().split('T')[0]}.csv`;
        document.body.appendChild(a);
        a.click();
        document.body.removeChild(a);
        window.URL.revokeObjectURL(url);

        this.showToast('Tenants exported successfully', 'success');
    }

    convertToCSV(data) {
        const headers = ['Name', 'Tenant ID', 'Astra Key', 'Status', 'Agent Count', 'Description', 'Created', 'Updated'];
        const rows = data.map(tenant => [
            tenant.name,
            tenant.tenant_id,
            tenant.astra_key,
            tenant.is_enabled ? 'Active' : 'Disabled',
            tenant.agent_count || 0,
            tenant.description || '',
            this.formatDate(tenant.created_at),
            this.formatDate(tenant.updated_at)
        ]);

        const csvContent = [headers, ...rows]
            .map(row => row.map(field => `"${String(field).replace(/"/g, '""')}"`).join(','))
            .join('\n');

        return csvContent;
    }

    setLoading(loading) {
        this.isLoading = loading;
        this.loadingState.style.display = loading ? 'block' : 'none';
        
        if (!loading) {
            this.renderTenants();
        }
    }

    closeModals() {
        this.closeTenantModal();
        this.closeDeleteModal();
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
        `;

        this.toastContainer.appendChild(toast);

        // Auto remove after duration
        setTimeout(() => {
            if (toast.parentNode) {
                toast.parentNode.removeChild(toast);
            }
        }, duration);

        // Click to dismiss
        toast.addEventListener('click', () => {
            if (toast.parentNode) {
                toast.parentNode.removeChild(toast);
            }
        });
    }
}

// Global functions for button onclick handlers
window.openTenantModal = () => tenantManager.openTenantModal();
window.closeTenantModal = () => tenantManager.closeTenantModal();
window.closeDeleteModal = () => tenantManager.closeDeleteModal();

// Initialize when DOM is loaded
let tenantManager;
document.addEventListener('DOMContentLoaded', () => {
    tenantManager = new TenantManager();
});
