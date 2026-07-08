// Custom topic filter: a styled, searchable dropdown that replaces the native
// <select>. Backed by a hidden <input name="tag"> so it plugs straight into the
// existing htmx wiring (the year filter and search box hx-include #tag-input).
// The filter bar lives outside the htmx-swapped #posts-list, so state set here
// persists across result swaps; a full page load re-renders the correct state.
(function () {
  function init(root) {
    if (root.dataset.topicReady === "1") return; // idempotent
    root.dataset.topicReady = "1";

    const trigger = root.querySelector(".topic-trigger");
    const panel = root.querySelector(".topic-panel");
    const label = root.querySelector(".topic-trigger-label");
    const input = root.querySelector(".topic-input");
    const search = root.querySelector(".topic-search");
    const options = Array.from(root.querySelectorAll(".topic-option"));
    if (!trigger || !panel || !label || !input || !search) return;

    function isOpen() {
      return !panel.hidden;
    }

    function open() {
      panel.hidden = false;
      trigger.setAttribute("aria-expanded", "true");
      search.value = "";
      filter("");
      search.focus();
    }

    function close() {
      panel.hidden = true;
      trigger.setAttribute("aria-expanded", "false");
    }

    function filter(q) {
      const needle = q.trim().toLowerCase();
      options.forEach((opt) => {
        const hay = (opt.dataset.label || "").toLowerCase();
        opt.hidden = needle !== "" && !hay.includes(needle);
      });
    }

    function visibleOptions() {
      return options.filter((o) => !o.hidden);
    }

    function select(value, text) {
      input.value = value;
      label.textContent = text;
      options.forEach((o) => o.classList.toggle("is-active", o.dataset.value === value));
      close();
      trigger.focus();
      // Fire the htmx request from the hidden input (carries tag; hx-include adds q + year)
      if (window.htmx) {
        window.htmx.trigger(input, "topic-changed");
      } else {
        input.dispatchEvent(new Event("topic-changed"));
      }
    }

    trigger.addEventListener("click", (e) => {
      e.stopPropagation();
      isOpen() ? close() : open();
    });

    search.addEventListener("input", () => filter(search.value));

    // Keyboard: Escape closes; ArrowDown from the search jumps into the list
    search.addEventListener("keydown", (e) => {
      if (e.key === "Escape") {
        close();
        trigger.focus();
      } else if (e.key === "ArrowDown") {
        e.preventDefault();
        const first = visibleOptions()[0];
        if (first) first.focus();
      }
    });

    options.forEach((opt) => {
      opt.addEventListener("click", () => select(opt.dataset.value, opt.dataset.label));
      opt.addEventListener("keydown", (e) => {
        const vis = visibleOptions();
        const i = vis.indexOf(opt);
        if (e.key === "ArrowDown") {
          e.preventDefault();
          (vis[i + 1] || vis[0]).focus();
        } else if (e.key === "ArrowUp") {
          e.preventDefault();
          if (i <= 0) search.focus();
          else vis[i - 1].focus();
        } else if (e.key === "Escape") {
          close();
          trigger.focus();
        }
      });
    });

    // Close on outside click / Escape anywhere
    document.addEventListener("click", (e) => {
      if (isOpen() && !root.contains(e.target)) close();
    });
    document.addEventListener("keydown", (e) => {
      if (e.key === "Escape" && isOpen()) close();
    });
  }

  function initAll() {
    document.querySelectorAll("[data-topic-filter]").forEach(init);
  }

  if (document.readyState !== "loading") initAll();
  else document.addEventListener("DOMContentLoaded", initAll);
})();
