// Navigation helper - shared across all pages that define a navData array.

(function () {
  'use strict';

  var selectedIndex = -1;

  function scrollToTop() {
    window.scrollTo({ top: 0, behavior: 'smooth' });
  }

  function openNavigator() {
    var modal = document.getElementById('nav-helper-modal');
    var input = document.getElementById('nav-helper-input');

    document.body.style.overflow = 'hidden';
    modal.style.display = 'flex';

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
      return '<a href="' + item.href + '"' +
        ' class="nav-helper-item' + (index === 0 ? ' selected' : '') + '"' +
        ' data-index="' + index + '"' +
        ' role="option"' +
        ' aria-selected="' + (index === 0) + '"' +
        ' onclick="closeNavigator()">' +
        '<span class="nav-helper-category">' + item.category + '</span>' +
        '<span class="nav-helper-name">' + item.label + '</span>' +
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
