/**
 * Pre-configured studio image templates with common AI/ML frameworks
 */

export interface StudioTemplate {
    id: string;
    name: string;
    image: string;
    description: string;
    category: 'pytorch' | 'custom';
    features: string[];
    defaultPorts?: string[];
    defaultEnv?: Record<string, string>;
    recommendedVolumes?: string[];
    icon: string;
    /** Recommended for beginners */
    recommended?: boolean;
    /** Difficulty level */
    level?: 'beginner' | 'intermediate' | 'advanced';
    /** Web UI URLs available after start */
    webUrls?: { name: string; port: number; path?: string }[];
}

export const STUDIO_TEMPLATES: StudioTemplate[] = [
    {
        id: 'pytorch-cuda12',
        name: 'PyTorch + Jupyter CUDA 12',
        image: 'quay.io/jupyter/pytorch-notebook:cuda12-python-3.11.8',
        description: 'Official Jupyter PyTorch notebook image with CUDA 12 and Python 3.11.8',
        category: 'pytorch',
        features: ['PyTorch', 'Jupyter Lab', 'CUDA 12', 'Python 3.11.8'],
        defaultPorts: ['8888:8888'],
        defaultEnv: { 'JUPYTER_ENABLE_LAB': 'yes', 'JUPYTER_TOKEN': '' },
        icon: '🔥',
        recommended: true,
        level: 'beginner',
        webUrls: [{ name: 'Jupyter Lab', port: 8888, path: '/lab' }]
    },
    {
        id: 'custom',
        name: 'Custom Image',
        image: '',
        description: 'Enter your own Docker image',
        category: 'custom',
        features: [],
        icon: '🔧',
        level: 'advanced'
    }
];

/**
 * Get templates by category
 */
export function getTemplatesByCategory(category: string): StudioTemplate[] {
    return STUDIO_TEMPLATES.filter(t => t.category === category);
}

/**
 * Get template by ID
 */
export function getTemplateById(id: string): StudioTemplate | undefined {
    return STUDIO_TEMPLATES.find(t => t.id === id);
}

/**
 * Get all categories
 */
export function getCategories(): Array<{ id: string; name: string; icon: string }> {
    return [
        { id: 'pytorch', name: 'PyTorch', icon: '🔥' },
        { id: 'custom', name: 'Custom Image', icon: '🔧' }
    ];
}

/**
 * Get recommended templates for beginners
 */
export function getRecommendedTemplates(): StudioTemplate[] {
    return STUDIO_TEMPLATES.filter(t => t.recommended);
}

/**
 * Get web URLs for a template
 */
export function getTemplateWebUrls(templateId: string): { name: string; port: number; path?: string }[] {
    const template = getTemplateById(templateId);
    return template?.webUrls || [];
}
