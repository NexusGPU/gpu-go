/**
 * Pre-configured studio image templates with common AI/ML frameworks
 */

export interface StudioTemplate {
    id: string;
    name: string;
    image: string;
    description: string;
    category: 'quickstart' | 'pytorch' | 'tensorflow' | 'jupyter' | 'rstudio' | 'general' | 'custom';
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
    // ⭐ Quick Start Templates (Recommended for Beginners)
    {
        id: 'quickstart-jupyter',
        name: '⭐ Jupyter Notebook (Recommended)',
        image: 'jupyter/scipy-notebook:latest',
        description: 'Best for beginners - Start coding in 1 click with Jupyter',
        category: 'quickstart',
        features: ['Jupyter Lab', 'Python 3', 'NumPy', 'Pandas', 'Matplotlib'],
        defaultPorts: ['8888:8888'],
        defaultEnv: { 'JUPYTER_ENABLE_LAB': 'yes', 'JUPYTER_TOKEN': '' },
        icon: '⭐',
        recommended: true,
        level: 'beginner',
        webUrls: [{ name: 'Jupyter Lab', port: 8888, path: '/lab' }]
    },
    {
        id: 'quickstart-pytorch',
        name: '⭐ PyTorch + Jupyter (Recommended)',
        image: 'jupyter/pytorch-notebook:latest',
        description: 'Best for deep learning - PyTorch with Jupyter ready to use',
        category: 'quickstart',
        features: ['PyTorch', 'Jupyter Lab', 'CUDA Ready', 'TensorBoard'],
        defaultPorts: ['8888:8888', '6006:6006'],
        defaultEnv: { 'JUPYTER_ENABLE_LAB': 'yes', 'JUPYTER_TOKEN': '' },
        icon: '⭐',
        recommended: true,
        level: 'beginner',
        webUrls: [
            { name: 'Jupyter Lab', port: 8888, path: '/lab' },
            { name: 'TensorBoard', port: 6006 }
        ]
    },
    {
        id: 'quickstart-tensorflow',
        name: '⭐ TensorFlow + Jupyter',
        image: 'jupyter/tensorflow-notebook:latest',
        description: 'TensorFlow with Jupyter - great for Keras tutorials',
        category: 'quickstart',
        features: ['TensorFlow', 'Keras', 'Jupyter Lab', 'CUDA Ready'],
        defaultPorts: ['8888:8888', '6006:6006'],
        defaultEnv: { 'JUPYTER_ENABLE_LAB': 'yes', 'JUPYTER_TOKEN': '' },
        icon: '⭐',
        recommended: true,
        level: 'beginner',
        webUrls: [
            { name: 'Jupyter Lab', port: 8888, path: '/lab' },
            { name: 'TensorBoard', port: 6006 }
        ]
    },

    // PyTorch Templates
    {
        id: 'pytorch-latest',
        name: 'PyTorch (Official)',
        image: 'pytorch/pytorch:latest',
        description: 'Official PyTorch image with CUDA support',
        category: 'pytorch',
        features: ['PyTorch', 'CUDA', 'Python 3', 'NumPy', 'SciPy'],
        defaultPorts: ['8888:8888', '6006:6006'],
        icon: '🔥',
        level: 'intermediate'
    },
    {
        id: 'pytorch-jupyter',
        name: 'PyTorch + Jupyter',
        image: 'jupyter/pytorch-notebook:latest',
        description: 'PyTorch with JupyterLab pre-installed',
        category: 'pytorch',
        features: ['PyTorch', 'Jupyter Lab', 'CUDA', 'Python 3'],
        defaultPorts: ['8888:8888', '6006:6006'],
        defaultEnv: { 'JUPYTER_ENABLE_LAB': 'yes', 'JUPYTER_TOKEN': '' },
        icon: '🔥',
        level: 'beginner',
        webUrls: [
            { name: 'Jupyter Lab', port: 8888, path: '/lab' },
            { name: 'TensorBoard', port: 6006 }
        ]
    },
    {
        id: 'tensorfusion-torch',
        name: 'TensorFusion PyTorch',
        image: 'tensorfusion/studio-torch:latest',
        description: 'TensorFusion optimized PyTorch with SSH and tools',
        category: 'pytorch',
        features: ['PyTorch', 'CUDA', 'SSH', 'Jupyter', 'TensorBoard', 'VS Code Server'],
        defaultPorts: ['8888:8888', '6006:6006'],
        icon: '🔥',
        level: 'intermediate',
        webUrls: [
            { name: 'Jupyter Lab', port: 8888, path: '/lab' },
            { name: 'TensorBoard', port: 6006 }
        ]
    },

    // TensorFlow Templates
    {
        id: 'tensorflow-gpu',
        name: 'TensorFlow GPU',
        image: 'tensorflow/tensorflow:latest-gpu',
        description: 'Official TensorFlow with GPU support',
        category: 'tensorflow',
        features: ['TensorFlow', 'CUDA', 'Python 3', 'Keras'],
        defaultPorts: ['8888:8888', '6006:6006'],
        icon: '🧠',
        level: 'intermediate',
        webUrls: [{ name: 'TensorBoard', port: 6006 }]
    },
    {
        id: 'tensorflow-jupyter',
        name: 'TensorFlow + Jupyter',
        image: 'jupyter/tensorflow-notebook:latest',
        description: 'TensorFlow with JupyterLab',
        category: 'tensorflow',
        features: ['TensorFlow', 'Jupyter Lab', 'CUDA', 'Python 3'],
        defaultPorts: ['8888:8888', '6006:6006'],
        defaultEnv: { 'JUPYTER_ENABLE_LAB': 'yes', 'JUPYTER_TOKEN': '' },
        icon: '🧠',
        level: 'beginner',
        webUrls: [
            { name: 'Jupyter Lab', port: 8888, path: '/lab' },
            { name: 'TensorBoard', port: 6006 }
        ]
    },
    {
        id: 'tensorfusion-tensorflow',
        name: 'TensorFusion TensorFlow',
        image: 'tensorfusion/studio-tensorflow:latest',
        description: 'TensorFusion optimized TensorFlow environment',
        category: 'tensorflow',
        features: ['TensorFlow', 'CUDA', 'SSH', 'Jupyter', 'TensorBoard'],
        defaultPorts: ['8888:8888', '6006:6006'],
        icon: '🧠',
        level: 'intermediate',
        webUrls: [
            { name: 'Jupyter Lab', port: 8888, path: '/lab' },
            { name: 'TensorBoard', port: 6006 }
        ]
    },

    // Jupyter/Data Science Templates
    {
        id: 'jupyter-scipy',
        name: 'Jupyter SciPy',
        image: 'jupyter/scipy-notebook:latest',
        description: 'Jupyter with scientific Python stack',
        category: 'jupyter',
        features: ['Jupyter Lab', 'NumPy', 'Pandas', 'Matplotlib', 'SciPy', 'scikit-learn'],
        defaultPorts: ['8888:8888'],
        defaultEnv: { 'JUPYTER_ENABLE_LAB': 'yes', 'JUPYTER_TOKEN': '' },
        icon: '📊',
        level: 'beginner',
        webUrls: [{ name: 'Jupyter Lab', port: 8888, path: '/lab' }]
    },
    {
        id: 'jupyter-datascience',
        name: 'Jupyter Data Science',
        image: 'jupyter/datascience-notebook:latest',
        description: 'Jupyter with Python, R, and Julia',
        category: 'jupyter',
        features: ['Jupyter Lab', 'Python', 'R', 'Julia', 'Pandas', 'scikit-learn'],
        defaultPorts: ['8888:8888'],
        defaultEnv: { 'JUPYTER_ENABLE_LAB': 'yes', 'JUPYTER_TOKEN': '' },
        icon: '📊',
        level: 'intermediate',
        webUrls: [{ name: 'Jupyter Lab', port: 8888, path: '/lab' }]
    },
    {
        id: 'jupyter-all-spark',
        name: 'Jupyter All-Spark',
        image: 'jupyter/all-spark-notebook:latest',
        description: 'Jupyter with Python, R, and Apache Spark',
        category: 'jupyter',
        features: ['Jupyter Lab', 'Python', 'R', 'Apache Spark', 'PySpark'],
        defaultPorts: ['8888:8888', '4040:4040'],
        defaultEnv: { 'JUPYTER_ENABLE_LAB': 'yes', 'JUPYTER_TOKEN': '' },
        icon: '📊',
        level: 'advanced',
        webUrls: [
            { name: 'Jupyter Lab', port: 8888, path: '/lab' },
            { name: 'Spark UI', port: 4040 }
        ]
    },

    // RStudio Templates
    {
        id: 'rstudio-base',
        name: 'RStudio',
        image: 'rocker/rstudio:latest',
        description: 'RStudio Server with R',
        category: 'rstudio',
        features: ['RStudio Server', 'R', 'tidyverse'],
        defaultPorts: ['8787:8787'],
        defaultEnv: { 'PASSWORD': 'rstudio' },
        icon: '📈',
        level: 'beginner',
        webUrls: [{ name: 'RStudio', port: 8787 }]
    },
    {
        id: 'rstudio-verse',
        name: 'RStudio Verse',
        image: 'rocker/verse:latest',
        description: 'RStudio with tidyverse and publishing tools',
        category: 'rstudio',
        features: ['RStudio Server', 'R', 'tidyverse', 'LaTeX', 'Pandoc'],
        defaultPorts: ['8787:8787'],
        defaultEnv: { 'PASSWORD': 'rstudio' },
        icon: '📈',
        level: 'intermediate',
        webUrls: [{ name: 'RStudio', port: 8787 }]
    },
    {
        id: 'rstudio-ml',
        name: 'RStudio ML',
        image: 'rocker/ml:latest',
        description: 'RStudio with machine learning packages',
        category: 'rstudio',
        features: ['RStudio Server', 'R', 'tidyverse', 'keras', 'tensorflow'],
        defaultPorts: ['8787:8787'],
        defaultEnv: { 'PASSWORD': 'rstudio' },
        icon: '📈',
        level: 'intermediate',
        webUrls: [{ name: 'RStudio', port: 8787 }]
    },

    // General Purpose Templates
    {
        id: 'miniconda',
        name: 'Miniconda',
        image: 'continuumio/miniconda3:latest',
        description: 'Minimal conda installation with Python 3',
        category: 'general',
        features: ['Python 3', 'conda', 'pip'],
        defaultPorts: ['8888:8888'],
        icon: '🐍',
        level: 'intermediate'
    },
    {
        id: 'anaconda',
        name: 'Anaconda',
        image: 'continuumio/anaconda3:latest',
        description: 'Full Anaconda distribution with 250+ packages',
        category: 'general',
        features: ['Python 3', 'conda', 'NumPy', 'Pandas', 'Matplotlib', 'scikit-learn'],
        defaultPorts: ['8888:8888'],
        icon: '🐍',
        level: 'beginner'
    },
    {
        id: 'python-base',
        name: 'Python 3.11',
        image: 'python:3.11-slim',
        description: 'Minimal Python 3.11 environment',
        category: 'general',
        features: ['Python 3.11', 'pip'],
        defaultPorts: ['8888:8888'],
        icon: '🐍',
        level: 'advanced'
    },
    {
        id: 'ubuntu-cuda',
        name: 'Ubuntu + CUDA',
        image: 'nvidia/cuda:12.2.0-devel-ubuntu22.04',
        description: 'Ubuntu 22.04 with CUDA 12.2 development tools',
        category: 'general',
        features: ['Ubuntu 22.04', 'CUDA 12.2', 'cuDNN', 'Development Tools'],
        icon: '🖥️',
        level: 'advanced'
    },
    {
        id: 'tensorfusion-full',
        name: 'TensorFusion Full Stack',
        image: 'tensorfusion/studio-full:latest',
        description: 'Complete AI development environment with all frameworks',
        category: 'general',
        features: ['PyTorch', 'TensorFlow', 'Jupyter', 'SSH', 'VS Code Server', 'CUDA'],
        defaultPorts: ['8888:8888', '6006:6006'],
        icon: '⚡',
        level: 'intermediate',
        webUrls: [
            { name: 'Jupyter Lab', port: 8888, path: '/lab' },
            { name: 'TensorBoard', port: 6006 }
        ]
    },

    // Custom
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
        { id: 'quickstart', name: '⭐ Quick Start (Recommended)', icon: '⭐' },
        { id: 'pytorch', name: 'PyTorch', icon: '🔥' },
        { id: 'tensorflow', name: 'TensorFlow', icon: '🧠' },
        { id: 'jupyter', name: 'Jupyter / Data Science', icon: '📊' },
        { id: 'rstudio', name: 'RStudio', icon: '📈' },
        { id: 'general', name: 'General Purpose', icon: '🐍' },
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
