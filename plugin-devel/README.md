# PLUGIN DEVELOPMENT

Base plugins are written in TypeScript and compiled to JavaScript but this is not a requirment of mcphe.

## Setup

```bash
cd plugin-devel
npm install
npm run build
```

## ADDING A NEW PLUGIN

You can add plugins in three ways:

1. Add a file to the plugins directory
2. Add a file to the plugin-devel directory
3. Add a plugin to a zip file and add the zip file to the plugins directory

## ADDING A PLUGIN TO THE CODEBASE

You can add a plugin in two ways:

1. Add a file to the plugins directory
2. Add a file to the plugin-devel directory

### Adding a file to the plugins directory

1. Create a new file in the plugins directory with the name of your plugin and the extension .ts or .js
2. Write your plugin code in the file
3. Compile the plugin if you wrote it in TypeScript
4. Restart mcphe to load the new plugin

### Adding a file to the plugin-devel directory

1. Create a new file in the plugin-devel directory with the name of your plugin and the extension .ts
2. Write your plugin code in the file
3. Compile the plugin by running `npm run build`
4. Restart mcphe to load the new plugin

## TESTING WITH VITEST

You can write tests for your plugins using Vitest. To run the tests, use the following command:

```bash
npm run test
```

This will run all the tests in the plugin-devel directory. You can also run specific tests by using the following command:

```bashnpm run test -- <test-file-name>
```

Replace `<test-file-name>` with the name of the test file you want to run.

## Deployment to Plugins Directory

```bash
npm run deploy
```

This will copy all the compiled JavaScript files from the plugin-devel directory to the plugins directory, making them available for mcphe to load requiring a restart of mcphe to load the new plugins.

## Makefile Commands

```bash
make plugin-tests        # builds and runs plugin tests
make plugin-deploy     # builds and deploys plugins to the plugins directory
```