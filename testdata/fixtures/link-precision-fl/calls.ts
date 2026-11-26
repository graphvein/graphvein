import axios from 'axios';
const RELEASES = 'https://api.github.com/repos/t034/t034/releases/latest';
export function origins(id: string) {
  fetch(RELEASES);
  axios.get(`https://plex.tv/api/v2/pins/${id}`);
  fetch('https://cdn.example.com/t034-feed.json', {method: 'POST'});
  const u = new URL('/t034-pages/home', 'https://example.com');
  u.host = window.location.host;
  fetch(u.toString());
  fetch(`https://api.example.com/t034-api/t034-items/${id}`);
  fetch('/t034-pages/about');
}
export function parameters(type: 'movie' | 'tv', id: string, slug: string) {
  axios.get(`/t034-api/t034-genres/${type}`);
  axios.get(`/t034-api/${type}/${id}`);
  axios.get('/t034-api/t034-genres/movie');
  axios.get(`/t034-api/t034-items/${id}`);
  fetch(`/t034-pages/${slug}`);
}
