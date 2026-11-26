// Adds the star count to the "Star" link once GitHub's public API answers,
// and only from data-star-min stars up. The link itself is always shown;
// on any failure it simply stays without a count. Loaded only when the
// public-repository switch is on.
(function () {
  var b = document.querySelector('[data-star-repo]');
  if (!b || !window.fetch) return;
  var min = parseInt(b.getAttribute('data-star-min'), 10) || 0;
  fetch('https://api.github.com/repos/' + b.getAttribute('data-star-repo'), {
    headers: { Accept: 'application/vnd.github+json' },
    credentials: 'omit',
    referrerPolicy: 'no-referrer'
  }).then(function (r) {
    if (!r.ok) throw new Error('status ' + r.status);
    return r.json();
  }).then(function (d) {
    var n = d && d.stargazers_count;
    if (typeof n !== 'number' || n < min) return;
    var c = b.querySelector('.star-count');
    c.textContent = n.toLocaleString('en');
    c.hidden = false;
    b.setAttribute('aria-label', 'Star Graphvein on GitHub, ' + n + ' stars');
  }).catch(function () {
    // No count: the link stays as it is.
  });
})();
