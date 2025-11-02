// Molecular network background animation
class MolecularNetwork {
    constructor() {
        this.canvas = document.getElementById('molecules-canvas');
        this.ctx = this.canvas.getContext('2d');
        this.particles = [];
        this.animationId = null;
        this.mouse = { x: null, y: null, radius: 200 };

        this.init();
    }

    init() {
        this.resize();
        this.createParticles();
        this.setupEventListeners();
        this.animate();
    }

    resize() {
        this.canvas.width = window.innerWidth;
        this.canvas.height = window.innerHeight;
    }

    createParticles() {
        const numberOfParticles = Math.floor((this.canvas.width * this.canvas.height) / 12000);

        for (let i = 0; i < numberOfParticles; i++) {
            const size = Math.random() * 2 + 1;
            const x = Math.random() * this.canvas.width;
            const y = Math.random() * this.canvas.height;
            const speedX = (Math.random() - 0.5) * 1.0;
            const speedY = (Math.random() - 0.5) * 1.0;

            this.particles.push({
                x, y, size, speedX, speedY,
                originalX: x,
                originalY: y,
                pulse: Math.random() * Math.PI * 2
            });
        }
    }

    setupEventListeners() {
        window.addEventListener('resize', () => {
            this.resize();
            this.particles = [];
            this.createParticles();
        });

        window.addEventListener('mousemove', (e) => {
            this.mouse.x = e.x;
            this.mouse.y = e.y;
        });

        window.addEventListener('mouseout', () => {
            this.mouse.x = null;
            this.mouse.y = null;
        });
    }

    getColors() {
        const theme = document.documentElement.getAttribute('data-theme');

        if (theme === 'dark') {
            return {
                particle: 'rgba(59, 130, 246, 0.5)',
                particleGlow: 'rgba(59, 130, 246, 0.15)',
                line: 'rgba(59, 130, 246, 0.12)'
            };
        } else {
            return {
                particle: 'rgba(37, 99, 235, 0.3)',
                particleGlow: 'rgba(37, 99, 235, 0.08)',
                line: 'rgba(37, 99, 235, 0.08)'
            };
        }
    }

    drawParticles() {
        const colors = this.getColors();

        for (let i = 0; i < this.particles.length; i++) {
            const p = this.particles[i];

            // Pulsing effect (more subtle)
            const pulseSize = Math.sin(p.pulse) * 0.3 + 1;
            const currentSize = p.size * pulseSize;

            // Draw subtle glow
            const gradient = this.ctx.createRadialGradient(p.x, p.y, 0, p.x, p.y, currentSize * 2);
            gradient.addColorStop(0, colors.particleGlow);
            gradient.addColorStop(1, 'rgba(59, 130, 246, 0)');

            this.ctx.fillStyle = gradient;
            this.ctx.beginPath();
            this.ctx.arc(p.x, p.y, currentSize * 2, 0, Math.PI * 2);
            this.ctx.fill();

            // Draw particle
            this.ctx.fillStyle = colors.particle;
            this.ctx.beginPath();
            this.ctx.arc(p.x, p.y, currentSize, 0, Math.PI * 2);
            this.ctx.fill();
        }
    }

    drawConnections() {
        const colors = this.getColors();
        const maxDistance = 130;

        for (let i = 0; i < this.particles.length; i++) {
            for (let j = i + 1; j < this.particles.length; j++) {
                const p1 = this.particles[i];
                const p2 = this.particles[j];

                const dx = p1.x - p2.x;
                const dy = p1.y - p2.y;
                const distance = Math.sqrt(dx * dx + dy * dy);

                if (distance < maxDistance) {
                    const opacity = 1 - (distance / maxDistance);

                    // Subtle, thin lines
                    this.ctx.strokeStyle = colors.line.replace('0.12', opacity * 0.15);
                    this.ctx.lineWidth = 1;
                    this.ctx.beginPath();
                    this.ctx.moveTo(p1.x, p1.y);
                    this.ctx.lineTo(p2.x, p2.y);
                    this.ctx.stroke();
                }
            }
        }
    }

    updateParticles() {
        for (let i = 0; i < this.particles.length; i++) {
            const p = this.particles[i];

            // Update pulse for animation
            p.pulse += 0.03;

            // Move particles
            p.x += p.speedX;
            p.y += p.speedY;

            // Bounce off edges
            if (p.x < 0 || p.x > this.canvas.width) p.speedX *= -1;
            if (p.y < 0 || p.y > this.canvas.height) p.speedY *= -1;

            // Stronger mouse interaction
            if (this.mouse.x !== null && this.mouse.y !== null) {
                const dx = this.mouse.x - p.x;
                const dy = this.mouse.y - p.y;
                const distance = Math.sqrt(dx * dx + dy * dy);

                if (distance < this.mouse.radius) {
                    const angle = Math.atan2(dy, dx);
                    const force = (this.mouse.radius - distance) / this.mouse.radius;

                    // Stronger repulsion force
                    p.x -= Math.cos(angle) * force * 5;
                    p.y -= Math.sin(angle) * force * 5;
                }
            }

            // Gentle pull back to original position
            const returnForce = 0.015;
            p.x += (p.originalX - p.x) * returnForce;
            p.y += (p.originalY - p.y) * returnForce;
        }
    }

    animate() {
        this.ctx.clearRect(0, 0, this.canvas.width, this.canvas.height);

        this.drawConnections();
        this.drawParticles();
        this.updateParticles();

        this.animationId = requestAnimationFrame(() => this.animate());
    }

    destroy() {
        if (this.animationId) {
            cancelAnimationFrame(this.animationId);
        }
        window.removeEventListener('resize', this.resize);
    }
}

// Initialize when DOM is ready
if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', () => {
        new MolecularNetwork();
    });
} else {
    new MolecularNetwork();
}
