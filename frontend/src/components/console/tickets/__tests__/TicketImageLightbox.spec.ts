import { defineComponent, nextTick, ref } from 'vue'
import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'

import i18n from '@/i18n'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import TicketImageLightbox from '@/components/console/tickets/TicketImageLightbox.vue'

// The marketplace listing dialog: a ConsoleModal with an image lightbox beside it.
const ListingWithImage = defineComponent({
  components: { ConsoleModal, TicketImageLightbox },
  setup() {
    return {
      modalOpen: ref(false),
      imageUrl: ref(''),
      lightboxMounted: ref(true),
    }
  },
  template: `
    <ConsoleModal :open="modalOpen" title="Listing" @close="modalOpen = false">
      <button>Add</button>
    </ConsoleModal>
    <TicketImageLightbox
      v-if="lightboxMounted"
      :open="imageUrl !== ''"
      :url="imageUrl"
      @close="imageUrl = ''"
    />
  `,
})

interface ListingState {
  modalOpen: boolean
  imageUrl: string
  lightboxMounted: boolean
}

function mountListing() {
  document.body.style.overflow = ''
  const wrapper = mount(ListingWithImage, {
    attachTo: document.body,
    global: { plugins: [i18n] },
  })
  return { wrapper, state: wrapper.vm as unknown as ListingState }
}

describe('TicketImageLightbox scroll lock', () => {
  it('keeps the page scrollable after an image is viewed over a modal and the page is left', async () => {
    const { wrapper, state } = mountListing()
    state.modalOpen = true
    await nextTick()
    state.imageUrl = '/api/ticket/image/1.png'
    await nextTick()
    expect(document.body.style.overflow).toBe('hidden')

    state.imageUrl = ''
    await nextTick()
    expect(document.body.style.overflow).toBe('hidden')
    state.modalOpen = false
    await nextTick()
    expect(document.body.style.overflow).toBe('')

    wrapper.unmount()
    expect(document.body.style.overflow).toBe('')
  })

  it('releases the page when it unmounts while the image is open over a modal', async () => {
    const { wrapper, state } = mountListing()
    state.modalOpen = true
    await nextTick()
    state.imageUrl = '/api/ticket/image/1.png'
    await nextTick()

    wrapper.unmount()
    expect(document.body.style.overflow).toBe('')
  })

  it('leaves an open modal locked when a lightbox that never opened unmounts', async () => {
    const { wrapper, state } = mountListing()
    state.modalOpen = true
    await nextTick()

    state.lightboxMounted = false
    await nextTick()
    expect(document.body.style.overflow).toBe('hidden')

    wrapper.unmount()
    expect(document.body.style.overflow).toBe('')
  })
})
