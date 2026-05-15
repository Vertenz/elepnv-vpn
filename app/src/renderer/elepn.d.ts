import  { type ElepnApi } from '../preload/api'

declare global {
  interface Window {
    elepn: ElepnApi
  }
}

export {}
