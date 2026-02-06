// Navigation helper - available on all pages.
// Pages can optionally set window.navData before this script loads to provide
// page-specific items. Global items are built from the sidebar menu and merged
// in, skipping any hrefs already present in the local data.

(function () {
  'use strict';

  var selectedIndex = -1;

  // Derive base path from this script's src attribute.
  var scriptEl = document.currentScript || document.querySelector('script[src*="nav-helper.js"]');
  var basePath = scriptEl ? scriptEl.src.replace(/\/assets\/js\/nav-helper\.js.*$/, '') : '';
  try { basePath = new URL(basePath).pathname.replace(/\/$/, ''); } catch (e) { basePath = ''; }

  // Build global nav items from the sidebar menu.
  function buildGlobalNavData() {
    var items = [];
    var sidebar = document.querySelector('.nav-tree');
    if (!sidebar) return items;

    sidebar.querySelectorAll('.nav-item').forEach(function (el) {
      var href = el.getAttribute('href');
      if (!href || href === '#') return;

      // Get the label text without the chevron.
      var label = el.textContent.trim();

      // Determine category from parent structure.
      var category = '';
      var parentLi = el.closest('.nav-parent');
      if (parentLi) {
        // Check if this element is the parent's own header (not a leaf).
        var directItem = parentLi.querySelector(':scope > .nav-item');
        if (directItem === el) return; // Skip parent headers, only include leaves.

        // Walk up to find the category from the closest parent header.
        var parentHeader = parentLi.querySelector(':scope > .nav-item');
        if (parentHeader) {
          category = parentHeader.textContent.trim();
        }

        // Check for grandparent category.
        var grandparent = parentLi.parentElement && parentLi.parentElement.closest('.nav-parent');
        if (grandparent) {
          var gpHeader = grandparent.querySelector(':scope > .nav-item');
          if (gpHeader) {
            category = gpHeader.textContent.trim() + ' / ' + category;
          }
        }
      }

      items.push({ label: label, category: category, href: href });
    });

    return items;
  }

  // Merge local and global nav data, deduplicating by href.
  var localData = window.navData || [];
  var globalData = buildGlobalNavData();
  var seenHrefs = {};

  localData.forEach(function (item) { seenHrefs[item.href] = true; });

  var merged = localData.concat(
    globalData.filter(function (item) {
      if (seenHrefs[item.href]) return false;
      seenHrefs[item.href] = true;
      return true;
    })
  );

  window.navData = merged;

  function scrollToTop() {
    window.scrollTo({ top: 0, behavior: 'smooth' });
  }

  function openNavigator() {
    var modal = document.getElementById('nav-helper-modal');
    var input = document.getElementById('nav-helper-input');

    document.body.style.overflow = 'hidden';
    modal.style.display = 'flex';

    input.value = '';

    setTimeout(function () {
      input.focus();
      filterNavigator();
    }, 50);
  }

  function closeNavigator() {
    var modal = document.getElementById('nav-helper-modal');

    document.body.style.overflow = '';
    modal.style.display = 'none';
    selectedIndex = -1;
  }

  function filterNavigator() {
    var input = document.getElementById('nav-helper-input');
    var results = document.getElementById('nav-helper-results');
    var query = input.value.toLowerCase().trim();
    var queryParts = query.split(/\s+/).filter(function (p) { return p.length > 0; });

    var filtered = window.navData.filter(function (item) {
      var searchText = (item.label + ' ' + item.category).toLowerCase();
      return queryParts.every(function (part) { return searchText.includes(part); });
    });

    if (filtered.length === 0) {
      results.innerHTML = '<div class="nav-helper-empty">No results found</div>';
      selectedIndex = -1;
      return;
    }

    results.innerHTML = filtered.map(function (item, index) {
      var isExternal = item.href.charAt(0) !== '#';
      var icon = isExternal
        ? ' <img src="' + basePath + '/assets/icons/external.svg" width="14" height="14" alt="" class="nav-helper-external">'
        : '';
      return '<a href="' + item.href + '"' +
        ' class="nav-helper-item' + (index === 0 ? ' selected' : '') + '"' +
        ' data-index="' + index + '"' +
        ' role="option"' +
        ' aria-selected="' + (index === 0) + '"' +
        ' onclick="closeNavigator()">' +
        '<span class="nav-helper-category">' + item.category + '</span>' +
        '<span class="nav-helper-name">' + item.label + icon + '</span>' +
        '</a>';
    }).join('');

    selectedIndex = 0;
  }

  function handleFilterKeyup(event) {
    var navKeys = ['ArrowDown', 'ArrowUp', 'Enter', 'Escape'];
    if (!navKeys.includes(event.key)) {
      filterNavigator();
    }
  }

  function handleNavigatorKeydown(event) {
    var results = document.getElementById('nav-helper-results');
    var items = results.querySelectorAll('.nav-helper-item');

    if (items.length === 0) return;

    switch (event.key) {
      case 'ArrowDown':
        event.preventDefault();
        selectedIndex = Math.min(selectedIndex + 1, items.length - 1);
        updateSelection(items);
        break;
      case 'ArrowUp':
        event.preventDefault();
        selectedIndex = Math.max(selectedIndex - 1, 0);
        updateSelection(items);
        break;
      case 'Enter':
        event.preventDefault();
        if (selectedIndex >= 0 && items[selectedIndex]) {
          items[selectedIndex].click();
        }
        break;
      case 'Escape':
        event.preventDefault();
        closeNavigator();
        break;
    }
  }

  function updateSelection(items) {
    items.forEach(function (item, index) {
      item.classList.toggle('selected', index === selectedIndex);
      item.setAttribute('aria-selected', index === selectedIndex);
    });
    scrollToSelected();
  }

  function scrollToSelected() {
    var results = document.getElementById('nav-helper-results');
    var selected = results.querySelector('.nav-helper-item.selected');

    if (selected) {
      var resultsRect = results.getBoundingClientRect();
      var selectedRect = selected.getBoundingClientRect();

      if (selectedRect.bottom > resultsRect.bottom || selectedRect.top < resultsRect.top) {
        selected.scrollIntoView({ block: 'nearest' });
      }
    }
  }

  // Global keyboard shortcuts.
  document.addEventListener('keydown', function (event) {
    var modal = document.getElementById('nav-helper-modal');

    if (event.key === 'Escape' && modal.style.display === 'flex') {
      closeNavigator();
      return;
    }

    if (event.key === 'k' && (event.ctrlKey || event.metaKey)) {
      event.preventDefault();
      openNavigator();
    }
  });

  // Back-to-top button visibility.
  var backToTopBtn = document.getElementById('back-to-top');

  function toggleBackToTop() {
    backToTopBtn.style.display = window.scrollY > window.innerHeight ? 'flex' : 'none';
  }

  window.addEventListener('scroll', toggleBackToTop);
  toggleBackToTop();

  // Prevent scroll on modal overlay.
  var overlay = document.querySelector('.nav-helper-overlay');
  if (overlay) {
    overlay.addEventListener('wheel', function (e) { e.preventDefault(); }, { passive: false });
    overlay.addEventListener('touchmove', function (e) { e.preventDefault(); }, { passive: false });
  }

  // Expose functions needed by inline onclick handlers.
  window.scrollToTop = scrollToTop;
  window.openNavigator = openNavigator;
  window.closeNavigator = closeNavigator;
  window.handleFilterKeyup = handleFilterKeyup;
  window.handleNavigatorKeydown = handleNavigatorKeydown;
})();
