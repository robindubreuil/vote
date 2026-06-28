const safeGet = (storage, key) => {
  try {
    return storage.getItem(key)
  } catch {
    return null
  }
}

const safeSet = (storage, key, value) => {
  try {
    storage.setItem(key, value)
  } catch {
  }
}

const safeRemove = (storage, key) => {
  try {
    storage.removeItem(key)
  } catch {
  }
}

export const safeLocalGet = (key) => safeGet(localStorage, key)
export const safeLocalSet = (key, value) => safeSet(localStorage, key, value)
export const safeLocalRemove = (key) => safeRemove(localStorage, key)
export const safeSessionGet = (key) => safeGet(sessionStorage, key)
export const safeSessionSet = (key, value) => safeSet(sessionStorage, key, value)
export const safeSessionRemove = (key) => safeRemove(sessionStorage, key)
