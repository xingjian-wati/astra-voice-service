// API Client with Key Validation
// This file provides a secure way to make API calls with key validation

(function() {
    'use strict';

    // Get API key from environment or global config
    // Priority: window.API_KEY > localStorage > prompt
    let API_KEY = null;
    
    function initializeAPIKey() {
        // Try to get from global config first
        if (window.API_KEY) {
            API_KEY = window.API_KEY;
            return;
        }
        
        // Try to get from localStorage
        API_KEY = localStorage.getItem('frontend_api_key');
        
        // If no key found, prompt user (only once per session)
        if (!API_KEY || API_KEY === 'null') {
            const userInput = prompt('请输入 key 验证:');
            if (userInput && userInput.trim()) {
                API_KEY = userInput.trim();
                localStorage.setItem('frontend_api_key', API_KEY);
            } else {
                console.error('Key is required to access this page');
                alert('请输入 key 验证。');
                return;
            }
        }
    }

    // Initialize API key on load
    initializeAPIKey();

    /**
     * Make an API request with automatic key validation
     * @param {string} url - API endpoint
     * @param {Object} options - Fetch options
     * @returns {Promise<Response>}
     */
    async function apiRequest(url, options = {}) {
        if (!API_KEY) {
            initializeAPIKey();
            if (!API_KEY) {
                throw new Error('API key not available');
            }
        }

        // Set default headers
        const headers = {
            'Content-Type': 'application/json',
            'X-API-Key': API_KEY,
            ...options.headers
        };

        // Create request options
        const requestOptions = {
            ...options,
            headers: headers
        };

        try {
            const response = await fetch(url, requestOptions);
            
            // Handle 401 Unauthorized - invalid key
            if (response.status === 401) {
                localStorage.removeItem('frontend_api_key');
                API_KEY = null;
                const errorMsg = await response.text();
                alert('Key 验证失败，请重新输入。');
                throw new Error('Unauthorized: ' + errorMsg);
            }
            
            // Handle other errors
            if (!response.ok) {
                const errorText = await response.text();
                throw new Error(`API Error ${response.status}: ${errorText}`);
            }
            
            return response;
        } catch (error) {
            console.error('API request failed:', error);
            throw error;
        }
    }

    /**
     * Convenience methods for common HTTP methods
     */
    const apiClient = {
        /**
         * GET request
         * @param {string} url 
         * @param {Object} options 
         * @returns {Promise<Response>}
         */
        get: async (url, options = {}) => {
            return apiRequest(url, {
                ...options,
                method: 'GET'
            });
        },

        /**
         * POST request
         * @param {string} url 
         * @param {Object} data 
         * @param {Object} options 
         * @returns {Promise<Response>}
         */
        post: async (url, data, options = {}) => {
            return apiRequest(url, {
                ...options,
                method: 'POST',
                body: JSON.stringify(data)
            });
        },

        /**
         * PUT request
         * @param {string} url 
         * @param {Object} data 
         * @param {Object} options 
         * @returns {Promise<Response>}
         */
        put: async (url, data, options = {}) => {
            return apiRequest(url, {
                ...options,
                method: 'PUT',
                body: JSON.stringify(data)
            });
        },

        /**
         * DELETE request
         * @param {string} url 
         * @param {Object} options 
         * @returns {Promise<Response>}
         */
        delete: async (url, options = {}) => {
            return apiRequest(url, {
                ...options,
                method: 'DELETE'
            });
        },

        /**
         * Update API key programmatically
         * @param {string} newKey 
         */
        setAPIKey: (newKey) => {
            API_KEY = newKey;
            if (newKey) {
                localStorage.setItem('frontend_api_key', newKey);
            } else {
                localStorage.removeItem('frontend_api_key');
            }
        },

        /**
         * Get current API key (returns null if not set)
         * @returns {string|null}
         */
        getAPIKey: () => {
            return API_KEY;
        }
    };

    // Export to global scope
    window.apiClient = apiClient;
})();

