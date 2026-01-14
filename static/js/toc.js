// Table of Contents Generator
(function() {
  'use strict';

  // Wait for DOM to be ready
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', initTOC);
  } else {
    initTOC();
  }

  function initTOC() {
    const content = document.getElementById('post-content');
    const tocList = document.getElementById('toc-list');
    const tocSidebar = document.getElementById('toc-sidebar');

    if (!content || !tocList || !tocSidebar) return;

    // Extract headings (h2 and h3)
    const headings = content.querySelectorAll('h2, h3');

    if (headings.length === 0) {
      // Hide TOC if no headings
      tocSidebar.style.display = 'none';
      return;
    }

    // Generate TOC
    headings.forEach((heading, index) => {
      // Add ID to heading if it doesn't have one
      if (!heading.id) {
        heading.id = `heading-${index}`;
      }

      const li = document.createElement('li');
      li.className = heading.tagName === 'H3' ? 'toc-item toc-item-h3' : 'toc-item';

      const a = document.createElement('a');
      a.href = `#${heading.id}`;
      a.textContent = heading.textContent;
      a.className = 'toc-link';

      // Smooth scroll on click
      a.addEventListener('click', (e) => {
        e.preventDefault();
        heading.scrollIntoView({ behavior: 'smooth', block: 'start' });

        // Update URL without jumping
        history.pushState(null, null, `#${heading.id}`);
      });

      li.appendChild(a);
      tocList.appendChild(li);
    });

    // Highlight active section on scroll
    let ticking = false;
    window.addEventListener('scroll', () => {
      if (!ticking) {
        window.requestAnimationFrame(() => {
          updateActiveSection(headings);
          ticking = false;
        });
        ticking = true;
      }
    });

    // Initial active section
    updateActiveSection(headings);
  }

  function updateActiveSection(headings) {
    const scrollPos = window.scrollY + 100; // Offset for header

    let activeHeading = null;
    headings.forEach((heading) => {
      if (heading.offsetTop <= scrollPos) {
        activeHeading = heading;
      }
    });

    // Remove all active classes
    document.querySelectorAll('.toc-link').forEach(link => {
      link.classList.remove('active');
    });

    // Add active class to current section
    if (activeHeading) {
      const activeLink = document.querySelector(`.toc-link[href="#${activeHeading.id}"]`);
      if (activeLink) {
        activeLink.classList.add('active');
      }
    }
  }
})();
