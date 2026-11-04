// Placeholder for useToast hook
// TODO: Implement proper toast hook with state management

export function useToast() {
  return {
    toasts: [],
    toast: (props) => console.log('Toast:', props),
  };
}

export const toast = (props) => console.log('Toast:', props);
