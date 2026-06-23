import { API_BASE_URL, ApiError } from './client';

function redirectToLogin() {
    if (window.location.pathname !== '/login') window.location.replace('/login');
}

async function operatorRequest(path, options = {}) {
    const { suppressOperatorRedirect = false, headers = {}, ...fetchOptions } = options;
    const response = await fetch(`${API_BASE_URL}${path}`, {
        credentials: 'include',
        ...fetchOptions,
        headers: {
            'Content-Type': 'application/json',
            'X-Themisto-Operator': 'console',
            ...headers,
        },
    });
    const data = await response.json().catch(() => null);
    if (!response.ok) {
        const error = new ApiError(data?.error || `Request failed: ${response.status}`, {
            status: response.status,
            code: data?.code,
            data,
        });
        if (response.status === 401 && !suppressOperatorRedirect) redirectToLogin();
        throw error;
    }
    return data;
}

export const operatorApi = {
    login: (accessKey) => operatorRequest('/api/v1/operator/auth/login', { method: 'POST', body: JSON.stringify({ access_key: accessKey }), suppressOperatorRedirect: true }),
    logout: () => operatorRequest('/api/v1/operator/auth/logout', { method: 'POST', suppressOperatorRedirect: true }),
    me: () => operatorRequest('/api/v1/operator/auth/me', { suppressOperatorRedirect: true }),
    listOrgs: () => operatorRequest('/api/v1/operator/orgs'),
    fleet: (params = {}) => {
        const query = new URLSearchParams(Object.fromEntries(Object.entries(params).filter(([, value]) => value))).toString();
        return operatorRequest(`/api/v1/operator/fleet${query ? `?${query}` : ''}`);
    },
    createOrg: (data) => operatorRequest('/api/v1/operator/orgs', { method: 'POST', body: JSON.stringify(data) }),
    updateProvisioning: (orgID, data) => operatorRequest(`/api/v1/operator/orgs/${orgID}/provisioning`, { method: 'PUT', body: JSON.stringify(data) }),
    createDeploymentPackage: (orgID, data) => operatorRequest(`/api/v1/operator/orgs/${orgID}/deployment-package`, { method: 'POST', body: JSON.stringify(data) }),
    updateStatus: (orgID, data) => operatorRequest(`/api/v1/operator/orgs/${orgID}/status`, { method: 'PUT', body: JSON.stringify(data) }),
    revokeOrgCerts: (orgID) => operatorRequest(`/api/v1/operator/orgs/${orgID}/revoke-certs`, { method: 'POST' }),
};
