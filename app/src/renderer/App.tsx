import { Badge, Button, Flex, Heading, Text } from '@radix-ui/themes'

export default function App() {
  return (
    <main className="app-shell">
      <Flex direction="column" gap="5">
        <Flex align="center" gap="3" wrap="wrap">
          <Heading as="h1" size="8">
            elepn
          </Heading>
          <Badge color="blue" variant="soft">
            Radix Themes
          </Badge>
        </Flex>

        <Text as="p" className="app-description" color="gray" size="4">
          Electron, Vite, React and TypeScript are ready for the first secure VPN client workflow.
        </Text>

        <Flex gap="3" wrap="wrap">
          <Button size="3">Connect</Button>
          <Button color="gray" size="3" variant="soft">
            Settings
          </Button>
        </Flex>
      </Flex>
    </main>
  )
}
