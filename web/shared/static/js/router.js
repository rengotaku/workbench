// The router switches between screens without reloading the page, so what a
// screen holds (the open row, the scroll position, a half-typed memo) is still
// there when you come back (ADR: the screens are one page).
//
// A screen joins by calling workbench.router.register(name, view) from its
// script. Until it does, links to it load the page as before: a screen whose
// script still runs on load and listens on document cannot sit hidden beside
// another, so the router only switches between registered screens and falls
// back to a normal navigation for everything else. That lets the screens move
// over one at a time.
(function () {
  'use strict';

  // The screen each path shows. The server answers every one of them.
  var ROUTES = {
    '/': 'todo',
    '/add': 'todo-add',
    '/notes': 'note-add',
    '/notes/timeline': 'timeline',
  };

  // The header link that marks each screen as current. The todo add screen
  // belongs to the todo list.
  var NAV = {
    'todo': '/',
    'todo-add': '/',
    'note-add': '/notes',
    'timeline': '/notes/timeline',
  };

  var views = {}; // name -> { view, mounted, scrollY }
  var current = null; // name of the screen on show
  var shownPath = ''; // its path and query, to tell a hash-only move apart
  var started = false;

  function viewFor(pathname) {
    return Object.prototype.hasOwnProperty.call(ROUTES, pathname) ? ROUTES[pathname] : null;
  }

  function rootOf(name) {
    return document.querySelector('[data-view="' + name + '"]');
  }

  // A screen can be switched to in place when its script registered it and
  // its root is in this document.
  function switchable(name) {
    return !!(name && views[name] && rootOf(name));
  }

  // register adds a screen. view has:
  //   title           the document title while it is shown
  //   mount(root)     builds the screen once, the first time it is shown
  //   show(url)       each time it is shown (url is a URL object)
  //   hide()          each time another screen replaces it
  function register(name, view) {
    views[name] = { view: view, mounted: false, scrollY: 0 };
  }

  function markNav(name) {
    var href = NAV[name];
    var links = document.querySelectorAll('.site-nav a');
    for (var i = 0; i < links.length; i++) {
      if (links[i].getAttribute('href') === href) {
        links[i].setAttribute('aria-current', 'page');
      } else {
        links[i].removeAttribute('aria-current');
      }
    }
  }

  function hideCurrent() {
    if (!current) return;
    var entry = views[current];
    entry.scrollY = window.scrollY;
    rootOf(current).hidden = true;
    if (entry.view.hide) entry.view.hide();
  }

  function showView(name, url, restoreScroll) {
    var entry = views[name];
    var root = rootOf(name);
    root.hidden = false;
    if (!entry.mounted) {
      entry.mounted = true;
      if (entry.view.mount) entry.view.mount(root);
    }
    current = name;
    shownPath = url.pathname + url.search;
    if (entry.view.title) document.title = entry.view.title;
    markNav(name);
    if (entry.view.show) entry.view.show(url);
    // The window scroll is shared by every screen, so each one keeps its own.
    window.scrollTo(0, restoreScroll ? entry.scrollY : 0);
  }

  // navigate shows the screen for href. It returns false when it left the
  // page to the browser (the target cannot be switched to in place).
  function navigate(href, opts) {
    opts = opts || {};
    var url = new URL(href, location.href);
    var target = viewFor(url.pathname);
    if (url.origin !== location.origin || !switchable(current) || !switchable(target)) {
      if (opts.replace) location.replace(url.href); else location.assign(url.href);
      return false;
    }
    var state = { view: target };
    if (opts.replace) history.replaceState(state, '', url.href);
    else history.pushState(state, '', url.href);
    if (target !== current) hideCurrent();
    showView(target, url, !!opts.restoreScroll);
    return true;
  }

  // A plain left click on a same-origin link to a switchable screen stays in
  // the page. Anything the browser does differently with modifiers, other
  // targets or downloads is left to the browser.
  document.addEventListener('click', function (e) {
    if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    var a = e.target.closest && e.target.closest('a[href]');
    if (!a || (a.target && a.target !== '_self') || a.hasAttribute('download')) return;
    var url = new URL(a.href, location.href);
    if (url.origin !== location.origin) return;
    // A link that only changes the hash on the same screen is the screen's own.
    if (url.pathname === location.pathname && url.search === location.search && url.hash) return;
    if (!switchable(current) || !switchable(viewFor(url.pathname))) return;
    e.preventDefault();
    navigate(url.href, { restoreScroll: true });
  });

  window.addEventListener('popstate', function () {
    // A screen that still runs on its own handles its history (a hash change)
    // itself; the router has nothing to switch.
    if (!switchable(current)) return;
    var target = viewFor(location.pathname);
    if (!switchable(target)) {
      // The entry belongs to a screen this page cannot show in place.
      location.reload();
      return;
    }
    var url = new URL(location.href);
    // Same screen, same path and query: only the hash moved, which is the
    // screen's own business (e.g. /#T007).
    if (target === current && url.pathname + url.search === shownPath) return;
    if (target !== current) hideCurrent();
    showView(target, url, true);
  });

  // start shows the screen the page was loaded for, once every script has had
  // the chance to register (deferred scripts run before DOMContentLoaded).
  function start() {
    if (started) return;
    started = true;
    var name = viewFor(location.pathname);
    if (!switchable(name)) return; // a screen that still runs on its own
    // The router restores each screen's scroll itself once it switches.
    if ('scrollRestoration' in history) history.scrollRestoration = 'manual';
    history.replaceState({ view: name }, '', location.href);
    showView(name, new URL(location.href), false);
  }

  // Deferred scripts run while readyState is already 'interactive', before
  // the scripts after this one have registered, so wait for the event unless
  // the page has finished loading.
  if (document.readyState === 'complete') {
    start();
  } else {
    document.addEventListener('DOMContentLoaded', start);
  }

  window.workbench = window.workbench || {};
  window.workbench.router = {
    register: register,
    navigate: navigate,
    current: function () { return current; },
  };
})();
