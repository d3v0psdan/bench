import { clsx, type ClassValue } from "clsx"
import { twMerge } from "tailwind-merge"

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

// macOS keeps its traffic lights and uses ⌘ shortcuts; Windows and Linux
// get our own window controls and Ctrl.
export const IS_MAC = navigator.userAgent.includes("Mac")
