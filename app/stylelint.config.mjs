export default {
  extends: ['stylelint-config-standard', 'stylelint-config-recess-order'],
  rules: {
    'alpha-value-notation': 'number',
    'color-function-notation': 'modern',
    'custom-property-pattern': '^([a-z][a-z0-9]*)(-[a-z0-9]+)*$',
    'selector-class-pattern': '^([a-z][a-z0-9]*)(-[a-z0-9]+)*$',
  },
}
