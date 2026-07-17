import axios from 'axios'

export const api = axios.create({
  baseURL: '',
  withCredentials: true,
  headers: {
    'Cache-Control': 'no-store',
  },
})

api.interceptors.request.use((config) => {
  const userId = window.localStorage.getItem('uid')
  if (userId) config.headers.set('New-Api-User', userId)
  return config
})
