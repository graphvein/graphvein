// The site's one first-party script (no inline scripts: the content
// security policy allows only 'self').
//
// 1. Theme. The page follows the system theme unless the reader picks
//    Light or Dark in the header; the choice is kept per browser in
//    localStorage and read back here. This file is loaded in <head>
//    without defer, so data-theme is set before the body is drawn and the
//    page does not flash in the wrong theme. Without the script, or
//    without storage, the page follows the system.
(function () {
  var KEY = 'graphvein-theme';
  var root = document.documentElement;

  function stored() {
    try {
      var v = window.localStorage.getItem(KEY);
      return v === 'light' || v === 'dark' ? v : 'system';
    } catch (e) {
      return 'system';
    }
  }

  function save(choice) {
    try {
      if (choice === 'system') window.localStorage.removeItem(KEY);
      else window.localStorage.setItem(KEY, choice);
    } catch (e) {
      // Storage refused (private mode, blocked site data): the choice
      // holds for this page only.
    }
  }

  function apply(choice) {
    if (choice === 'light' || choice === 'dark') root.setAttribute('data-theme', choice);
    else root.removeAttribute('data-theme');
    var buttons = document.querySelectorAll('[data-theme-choice]');
    for (var i = 0; i < buttons.length; i++) {
      buttons[i].setAttribute('aria-pressed', String(buttons[i].getAttribute('data-theme-choice') === choice));
    }
  }

  apply(stored());

  function setup() {
    var group = document.querySelector('.theme');
    if (!group) return;
    apply(stored());
    group.hidden = false;
    group.addEventListener('click', function (e) {
      var button = e.target.closest && e.target.closest('[data-theme-choice]');
      if (!button) return;
      var choice = button.getAttribute('data-theme-choice');
      save(choice);
      apply(choice);
    });
    // Another tab of the site changed the choice.
    window.addEventListener('storage', function (e) {
      if (e.key === KEY || e.key === null) apply(stored());
    });
  }

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', setup);
  else setup();
})();

// 2. Screenshot lightbox. Opens a feature screenshot in a lightbox: the
// whole, full-resolution image over a dark backdrop, in the same theme
// variant as the thumbnail that was clicked. Closes on Esc, the close
// button or a click outside the image, and returns focus to the link that
// opened it. Without this script each screenshot is a plain link to the
// image.
(function () {
  if (typeof HTMLDialogElement !== 'function') return;
  var dialog = null;
  var opener = null;

  function build() {
    dialog = document.createElement('dialog');
    dialog.className = 'lightbox';
    dialog.setAttribute('aria-modal', 'true');
    dialog.setAttribute('aria-label', 'Screenshot');
    var close = document.createElement('button');
    close.type = 'button';
    close.className = 'lb-close';
    close.textContent = 'Close';
    close.addEventListener('click', function () { dialog.close(); });
    var fig = document.createElement('figure');
    var img = document.createElement('img');
    var cap = document.createElement('figcaption');
    fig.appendChild(img);
    fig.appendChild(cap);
    dialog.appendChild(close);
    dialog.appendChild(fig);
    dialog.addEventListener('click', function (e) {
      if (e.target === dialog || e.target === fig) dialog.close();
    });
    dialog.addEventListener('close', function () {
      if (opener) opener.focus();
    });
    document.body.appendChild(dialog);
  }

  document.addEventListener('click', function (e) {
    if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    var link = e.target.closest && e.target.closest('a.zoom');
    if (!link) return;
    e.preventDefault();
    if (!dialog) build();
    opener = link;
    var thumb = link.querySelector('img');
    var img = dialog.querySelector('img');
    img.src = link.getAttribute('href');
    img.alt = thumb ? thumb.alt : '';
    var fig = link.closest('figure');
    var caption = fig && fig.querySelector('figcaption');
    dialog.querySelector('figcaption').textContent = caption ? caption.textContent : '';
    dialog.showModal();
    dialog.querySelector('.lb-close').focus();
  });
})();
