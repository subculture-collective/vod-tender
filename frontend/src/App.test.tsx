import { describe, it, expect } from 'vitest'
import { render, screen, waitFor } from './test/utils'
import userEvent from '@testing-library/user-event'
import App from './App'

describe('App', () => {
  it('renders the header', () => {
    render(<App />)
    expect(screen.getByText('VOD Tender Dashboard')).toBeInTheDocument()
  })

  it('renders the footer with current year', () => {
    render(<App />)
    const currentYear = new Date().getFullYear()
    expect(
      screen.getByText(new RegExp(currentYear.toString()))
    ).toBeInTheDocument()
  })

  it('initially shows VOD list', async () => {
    render(<App />)

    // Wait for VOD list to load
    await waitFor(() => {
      expect(screen.getByText('Twitch VODs')).toBeInTheDocument()
    })
  })

  it('navigates to VOD detail when a VOD is selected', async () => {
    const user = userEvent.setup()
    render(<App />)

    // Wait for VOD list to load
    await waitFor(() => {
      expect(screen.getByText('Test VOD 1')).toBeInTheDocument()
    })

    // Click on a VOD
    const vodRow = screen.getByText('Test VOD 1').closest('tr')
    expect(vodRow).toBeInTheDocument()

    if (vodRow) {
      await user.click(vodRow)
    }

    // Should navigate to detail view
    await waitFor(() => {
      expect(screen.getByText('Test VOD 1')).toBeInTheDocument()
      expect(screen.getByText(/back to list/i)).toBeInTheDocument()
    })
  })

  it('navigates back to list from detail view', async () => {
    const user = userEvent.setup()
    render(<App />)

    // Wait for VOD list and click on a VOD
    await waitFor(() => {
      expect(screen.getByText('Test VOD 1')).toBeInTheDocument()
    })

    const vodRow = screen.getByText('Test VOD 1').closest('tr')
    if (vodRow) {
      await user.click(vodRow)
    }

    // Wait for detail view
    await waitFor(() => {
      expect(screen.getByText(/back to list/i)).toBeInTheDocument()
    })

    // Click back button
    const backButton = screen.getByText(/back to list/i)
    await user.click(backButton)

    // Should be back at list view
    await waitFor(() => {
      expect(screen.getByText('Twitch VODs')).toBeInTheDocument()
    })
  })
})
