export type ValidationRule = {
  test: (value: string) => boolean;
  message: string;
};

export const required: ValidationRule = {
  test: (v) => v.trim() !== '',
  message: 'This field is required',
};

export const email: ValidationRule = {
  test: (v) => /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(v),
  message: 'Please enter a valid email address',
};

export const minLength = (min: number): ValidationRule => ({
  test: (v) => v.length >= min,
  message: `Must be at least ${min} characters`,
});

export const maxLength = (max: number): ValidationRule => ({
  test: (v) => v.length <= max,
  message: `Must be no more than ${max} characters`,
});

export const matches = (pattern: RegExp): ValidationRule => ({
  test: (v) => pattern.test(v),
  message: 'Format is invalid',
});

export function validateField(value: string, rules: ValidationRule[]) {
  for (const rule of rules) {
    if (!rule.test(value)) return rule.message;
  }
  return null;
}